// Package divoom is a LAN HTTP client for a Divoom Times Frame device. It mirrors
// the firmware-strict wire format used by the reference MCP server: JSON commands
// on /divoom_api and a two-part multipart body (JSON part first, single file
// second, per-part Content-Length, CRLF, unquoted boundary) for file uploads.
package divoom

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	endpointAPI       = "/divoom_api"
	endpointCreate    = "/create_local_clock"
	endpointReplaceBg = "/replace_clock_dial_bg"
	endpointPatch     = "/patch_local_clock"

	boundaryCreate    = "----GoDivoomCreateClockBoundary7YA4YWxkTrZu0gW"
	boundaryReplaceBg = "----GoDivoomReplaceBgBoundary7YA4YWxkTrZu0gW"
	boundaryPatch     = "----GoDivoomPatchClockBoundary7YA4YWxkTrZu0gW"

	// maxBackgroundBytes mirrors DIVOOM_REPLACE_DIAL_BG_MAX_FILE_BYTES.
	maxBackgroundBytes = 500 * 1024
)

// Client talks to a single device over HTTP.
type Client struct {
	baseURL string
	httpc   *http.Client
}

// Option customises a Client.
type Option func(*Client)

// WithHTTPClient overrides the underlying HTTP client (used in tests).
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.httpc = h }
}

// WithBaseURL overrides the device base URL (used in tests).
func WithBaseURL(u string) Option {
	return func(c *Client) { c.baseURL = u }
}

// New returns a client for the device at host:port with the given per-request
// timeout.
func New(host string, port int, timeout time.Duration, opts ...Option) *Client {
	c := &Client{
		baseURL: fmt.Sprintf("http://%s:%d", host, port),
		httpc:   &http.Client{Timeout: timeout},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

type apiResponse struct {
	ReturnCode    int    `json:"ReturnCode"`
	ReturnMessage string `json:"ReturnMessage"`
	ClockID       int    `json:"ClockId"`
	Brightness    int    `json:"Brightness"`
}

// Glow modes for AmbientLight.SelectEffect, verified visually on a Times Frame.
// The firmware silently stores a value outside 0–7 as 0 (bottom-only), so
// callers must keep SelectEffect in range rather than rely on an error.
const (
	AmbientEffectBottomOnly = 0 // only the lower part of the strip, static
	AmbientEffectSolid      = 1 // whole strip, static
	AmbientEffectWaveUp     = 2 // wave travelling bottom to top
	AmbientEffectBreathe    = 3 // slow shimmer
	AmbientEffectFlicker    = 4 // shimmer, noticeably busier than breathe
	AmbientEffectRunnerDown = 5 // small runner moving top to bottom
	AmbientEffectFadeInOut  = 6 // whole strip fading in and out
	AmbientEffectStatic     = 7 // whole strip, no animation (looks like solid)

	// AmbientEffectMax is the highest value the firmware accepts.
	AmbientEffectMax = AmbientEffectStatic
)

// AmbientLight is the state of the vertical RGB strip on the side of the case.
// Channel/GetAmbientLight and Channel/SetAmbientLight are undocumented but
// present in the Times Frame firmware (found by probing) and are the only way to
// control the strip — the Divoom app exposes no setting for it.
//
// A Set always writes every field: sending Channel/SetAmbientLight with fields
// missing zeroes the whole structure on the device, which is why the payload is
// assembled explicitly below.
type AmbientLight struct {
	Brightness   int    `json:"Brightness"`   // 0–100; 0 blanks the strip
	Color        string `json:"Color"`        // "#rrggbb"; the base colour when ColorCycle is on
	ColorCycle   int    `json:"ColorCycle"`   // 1 = drift through the spectrum
	EqOnOff      int    `json:"EqOnOff"`      // 1 = react to sound
	SelectEffect int    `json:"SelectEffect"` // glow mode, see AmbientEffect*
}

// SameAs reports whether two strip states are equivalent. It tolerates the case
// of the colour, which the device echoes back lowercased.
func (l AmbientLight) SameAs(o AmbientLight) bool {
	return l.Brightness == o.Brightness &&
		l.ColorCycle == o.ColorCycle &&
		l.EqOnOff == o.EqOnOff &&
		l.SelectEffect == o.SelectEffect &&
		strings.EqualFold(l.Color, o.Color)
}

// On reports whether the strip is lit in this state.
func (l AmbientLight) On() bool { return l.Brightness > 0 }

func (l AmbientLight) validate() error {
	if l.Brightness < 0 || l.Brightness > 100 {
		return fmt.Errorf("ambient brightness %d out of range (0-100)", l.Brightness)
	}
	if l.SelectEffect < 0 || l.SelectEffect > AmbientEffectMax {
		return fmt.Errorf("ambient effect %d out of range (0-%d)", l.SelectEffect, AmbientEffectMax)
	}
	if l.ColorCycle != 0 && l.ColorCycle != 1 {
		return fmt.Errorf("ambient color cycle %d must be 0 or 1", l.ColorCycle)
	}
	if l.EqOnOff != 0 && l.EqOnOff != 1 {
		return fmt.Errorf("ambient eq %d must be 0 or 1", l.EqOnOff)
	}
	if !validHexColor(l.Color) {
		return fmt.Errorf("ambient color %q must be #rrggbb", l.Color)
	}
	return nil
}

// SetAmbientLight writes the full state of the side RGB strip.
func (c *Client) SetAmbientLight(ctx context.Context, l AmbientLight) error {
	if err := l.validate(); err != nil {
		return err
	}
	_, err := c.command(ctx, "Channel/SetAmbientLight", map[string]any{
		"Brightness":   l.Brightness,
		"Color":        l.Color,
		"ColorCycle":   l.ColorCycle,
		"EqOnOff":      l.EqOnOff,
		"SelectEffect": l.SelectEffect,
	})
	return err
}

// GetAmbientLight reads the current state of the side RGB strip. The state is
// persisted by the device, so it survives reboots and reads back what was set.
func (c *Client) GetAmbientLight(ctx context.Context) (AmbientLight, error) {
	const command = "Channel/GetAmbientLight"
	raw, err := c.postJSON(ctx, endpointAPI, map[string]any{"Command": command, "ReturnCode": 0})
	if err != nil {
		return AmbientLight{}, err
	}
	if _, err := parseResponse(command, raw); err != nil {
		return AmbientLight{}, err
	}
	var l AmbientLight
	if err := json.Unmarshal(raw, &l); err != nil {
		return AmbientLight{}, fmt.Errorf("%s: decode response %q: %w", command, truncate(raw), err)
	}
	return l, nil
}

func validHexColor(s string) bool {
	if len(s) != 7 || s[0] != '#' {
		return false
	}
	for _, r := range s[1:] {
		if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
			return false
		}
	}
	return true
}

// Ping issues a cheap read-only command to confirm the device is reachable.
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.command(ctx, "Sys/GetBrightness", nil)
	return err
}

// SetClockSelect switches the active on-screen dial.
func (c *Client) SetClockSelect(ctx context.Context, clockID int) error {
	_, err := c.command(ctx, "Channel/SetClockSelectId", map[string]any{"ClockId": clockID})
	return err
}

// SetBrightness sets the display brightness on the 0–100 scale.
func (c *Client) SetBrightness(ctx context.Context, level int) error {
	if level < 0 || level > 100 {
		return fmt.Errorf("brightness %d out of range (0-100)", level)
	}
	_, err := c.command(ctx, "Channel/SetBrightness", map[string]any{"Brightness": level})
	return err
}

// OnOffScreen turns the display on (true) or off (false) via
// Channel/OnOffScreen. A quick off→on cycle clears the device's wedged
// image-upload path after a run of failed background pushes (the same fix as
// power-cycling the screen by hand).
func (c *Client) OnOffScreen(ctx context.Context, on bool) error {
	v := 0
	if on {
		v = 1
	}
	_, err := c.command(ctx, "Channel/OnOffScreen", map[string]any{"OnOff": v})
	return err
}

// GetBrightness reads the current display brightness (0–100).
func (c *Client) GetBrightness(ctx context.Context) (int, error) {
	r, err := c.command(ctx, "Sys/GetBrightness", nil)
	if err != nil {
		return 0, err
	}
	return r.Brightness, nil
}

// CreateLocalClock creates a new local dial from a background image plus an
// ItemList (e.g. the native clock layer) and returns the new ClockId.
func (c *Client) CreateLocalClock(ctx context.Context, name string, itemList []map[string]any, itemIDList []string, background []byte) (int, error) {
	if err := validateBackground(background); err != nil {
		return 0, err
	}
	meta := map[string]any{
		"ClockName":  name,
		"ItemList":   itemList,
		"ItemIdList": itemIDList,
		"DialAssets": "image",
		"Command":    "Device/CreateLocalClock",
		"ReturnCode": 0,
	}
	raw, err := c.postMultipart(ctx, endpointCreate, meta, boundaryCreate, "clock_bg.jpg", background)
	if err != nil {
		return 0, err
	}
	r, err := parseResponse("Device/CreateLocalClock", raw)
	if err != nil {
		return 0, err
	}
	return r.ClockID, nil
}

// ReplaceDialBg replaces the cached background bitmap of a dial without touching
// its ItemList (the native clock layer keeps ticking). When clockID is 0 the
// currently displayed dial is targeted.
func (c *Client) ReplaceDialBg(ctx context.Context, clockID int, background []byte) error {
	if err := validateBackground(background); err != nil {
		return err
	}
	meta := map[string]any{
		"Command":    "Device/ReplaceClockDialBgFile",
		"ReturnCode": 0,
	}
	if clockID > 0 {
		meta["ClockId"] = clockID
	} else {
		meta["UseCurrentDisplayClock"] = 1
	}
	raw, err := c.postMultipart(ctx, endpointReplaceBg, meta, boundaryReplaceBg, "clock_bg.jpg", background)
	if err != nil {
		return err
	}
	_, err = parseResponse("Device/ReplaceClockDialBgFile", raw)
	return err
}

// PatchDialBg replaces the dial's actual stored backdrop via
// Device/PatchLocalClockInfo (multipart /patch_local_clock), preserving the
// ItemList (the native clock layer keeps ticking). Unlike ReplaceDialBg — whose
// cache the Times Frame does not show on the live display — this changes the
// stored background, so a following SetClockSelect makes the device redraw the
// new image. When clockID is 0 the currently displayed dial is targeted.
func (c *Client) PatchDialBg(ctx context.Context, clockID int, background []byte) error {
	if err := validateBackground(background); err != nil {
		return err
	}
	meta := map[string]any{
		"Command":    "Device/PatchLocalClockInfo",
		"ReturnCode": 0,
	}
	if clockID > 0 {
		meta["ClockId"] = clockID
	} else {
		meta["UseCurrentDisplayClock"] = 1
	}
	raw, err := c.postMultipart(ctx, endpointPatch, meta, boundaryPatch, "clock_bg.jpg", background)
	if err != nil {
		return err
	}
	_, err = parseResponse("Device/PatchLocalClockInfo", raw)
	return err
}

func (c *Client) command(ctx context.Context, command string, payload map[string]any) (apiResponse, error) {
	if payload == nil {
		payload = map[string]any{}
	}
	payload["Command"] = command
	payload["ReturnCode"] = 0

	raw, err := c.postJSON(ctx, endpointAPI, payload)
	if err != nil {
		return apiResponse{}, err
	}
	return parseResponse(command, raw)
}

func (c *Client) postJSON(ctx context.Context, endpoint string, payload map[string]any) ([]byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")
	req.ContentLength = int64(len(body))
	return c.do(req, endpoint)
}

func (c *Client) postMultipart(ctx context.Context, endpoint string, meta map[string]any, boundary, fileName string, file []byte) ([]byte, error) {
	body, err := buildMultipart(meta, file, fileName, boundary)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "multipart/form-data; boundary="+boundary)
	req.ContentLength = int64(len(body))
	return c.do(req, endpoint)
}

func (c *Client) do(req *http.Request, endpoint string) ([]byte, error) {
	resp, err := c.httpc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("post %s: %w", endpoint, err)
	}
	defer func() { _ = resp.Body.Close() }()

	out, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("read %s response: %w", endpoint, err)
	}
	if resp.StatusCode != http.StatusOK {
		return out, fmt.Errorf("post %s: http %d", endpoint, resp.StatusCode)
	}
	return out, nil
}

func parseResponse(command string, raw []byte) (apiResponse, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return apiResponse{}, fmt.Errorf("%s: empty device response", command)
	}
	var r apiResponse
	if err := json.Unmarshal(raw, &r); err != nil {
		return apiResponse{}, fmt.Errorf("%s: decode response %q: %w", command, truncate(raw), err)
	}
	if r.ReturnCode != 0 {
		return r, fmt.Errorf("%s: device returned code %d: %s", command, r.ReturnCode, r.ReturnMessage)
	}
	return r, nil
}

// buildMultipart assembles the firmware-strict two-part body: JSON part first
// (name="json", filename="cmd.json"), the single file second, each with its own
// Content-Length, CRLF throughout, closed by \r\n--boundary--\r\n.
func buildMultipart(meta map[string]any, file []byte, fileName, boundary string) ([]byte, error) {
	metaBytes, err := json.Marshal(meta)
	if err != nil {
		return nil, fmt.Errorf("marshal meta: %w", err)
	}
	const crlf = "\r\n"
	partName := strconv.FormatInt(time.Now().UnixMilli(), 10)

	var buf bytes.Buffer
	buf.Grow(len(metaBytes) + len(file) + 512)

	buf.WriteString("--" + boundary + crlf)
	buf.WriteString(`Content-Disposition: form-data; name="json"; filename="cmd.json"` + crlf)
	buf.WriteString("Content-Type: application/json" + crlf)
	buf.WriteString("Content-Length: " + strconv.Itoa(len(metaBytes)) + crlf + crlf)
	buf.Write(metaBytes)
	buf.WriteString(crlf)

	buf.WriteString("--" + boundary + crlf)
	buf.WriteString(`Content-Disposition: form-data; name="` + partName + `"; filename="` + fileName + `"` + crlf)
	buf.WriteString("Content-Type: application/octet-stream" + crlf)
	buf.WriteString("Content-Length: " + strconv.Itoa(len(file)) + crlf + crlf)
	buf.Write(file)

	buf.WriteString(crlf + "--" + boundary + "--" + crlf)
	return buf.Bytes(), nil
}

func validateBackground(b []byte) error {
	if len(b) == 0 {
		return fmt.Errorf("background image is empty")
	}
	if len(b) > maxBackgroundBytes {
		return fmt.Errorf("background too large: %d bytes (max %d)", len(b), maxBackgroundBytes)
	}
	switch {
	case len(b) >= 2 && b[0] == 0xFF && b[1] == 0xD8: // JPEG
		return nil
	case len(b) >= 12 && string(b[0:4]) == "RIFF" && string(b[8:12]) == "WEBP": // WebP
		return nil
	default:
		return fmt.Errorf("background must be JPEG (FF D8) or WebP (RIFF…WEBP)")
	}
}

func truncate(b []byte) string {
	const max = 200
	if len(b) > max {
		return string(b[:max]) + "…"
	}
	return string(b)
}

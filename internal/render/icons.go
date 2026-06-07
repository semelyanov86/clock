package render

import (
	"math"

	"github.com/fogleman/gg"
	"github.com/semelyanov86/clock/internal/model"
)

const (
	iconSun   = "#FBBF24"
	iconCloud = "#C2CCDA"
	iconRain  = "#5BA3F5"
	iconSnow  = "#E6EDF3"
	iconBolt  = "#FBBF24"
)

// drawWeatherIcon paints a simple vector weather icon centred at (cx, cy) and
// sized to roughly `size` pixels, chosen by the WMO weather code.
func drawWeatherIcon(dc *gg.Context, cx, cy, size float64, code int) {
	cat, _ := model.DescribeWMO(code)
	switch cat {
	case model.WeatherClear:
		drawSun(dc, cx, cy, size)
	case model.WeatherPartly:
		drawSun(dc, cx-0.18*size, cy-0.16*size, size*0.7)
		drawCloud(dc, cx+0.08*size, cy+0.12*size, size*0.95, iconCloud)
	case model.WeatherFog:
		drawCloud(dc, cx, cy-0.06*size, size, iconCloud)
		drawLines(dc, cx, cy+0.22*size, size, iconCloud)
	case model.WeatherRain:
		drawCloud(dc, cx, cy-0.08*size, size, iconCloud)
		drawDrops(dc, cx, cy, size)
	case model.WeatherSnow:
		drawCloud(dc, cx, cy-0.08*size, size, iconCloud)
		drawFlakes(dc, cx, cy, size)
	case model.WeatherThunder:
		drawCloud(dc, cx, cy-0.08*size, size, iconCloud)
		drawBolt(dc, cx, cy, size)
	default:
		drawCloud(dc, cx, cy, size, iconCloud)
	}
}

func drawSun(dc *gg.Context, cx, cy, size float64) {
	r := size * 0.22
	dc.SetHexColor(iconSun)
	dc.SetLineWidth(size * 0.045)
	for i := 0; i < 8; i++ {
		ang := float64(i) * math.Pi / 4
		x1 := cx + math.Cos(ang)*r*1.5
		y1 := cy + math.Sin(ang)*r*1.5
		x2 := cx + math.Cos(ang)*r*2.0
		y2 := cy + math.Sin(ang)*r*2.0
		dc.DrawLine(x1, y1, x2, y2)
	}
	dc.Stroke()
	dc.DrawCircle(cx, cy, r)
	dc.Fill()
}

func drawCloud(dc *gg.Context, cx, cy, size float64, fill string) {
	dc.SetHexColor(fill)
	dc.DrawCircle(cx-0.18*size, cy+0.04*size, 0.20*size)
	dc.DrawCircle(cx+0.18*size, cy+0.06*size, 0.18*size)
	dc.DrawCircle(cx, cy-0.10*size, 0.24*size)
	dc.DrawRoundedRectangle(cx-0.36*size, cy+0.02*size, 0.72*size, 0.18*size, 0.09*size)
	dc.Fill()
}

func drawDrops(dc *gg.Context, cx, cy, size float64) {
	dc.SetHexColor(iconRain)
	dc.SetLineWidth(size * 0.05)
	for i := -1; i <= 1; i++ {
		x := cx + float64(i)*0.18*size
		dc.DrawLine(x+0.04*size, cy+0.22*size, x-0.03*size, cy+0.40*size)
	}
	dc.Stroke()
}

func drawFlakes(dc *gg.Context, cx, cy, size float64) {
	dc.SetHexColor(iconSnow)
	for i := -1; i <= 1; i++ {
		x := cx + float64(i)*0.18*size
		dc.DrawCircle(x, cy+0.32*size, size*0.04)
	}
	dc.Fill()
}

func drawLines(dc *gg.Context, cx, cy, size float64, fill string) {
	dc.SetHexColor(fill)
	dc.SetLineWidth(size * 0.045)
	for i := 0; i < 3; i++ {
		y := cy + float64(i)*0.12*size
		dc.DrawLine(cx-0.32*size, y, cx+0.32*size, y)
	}
	dc.Stroke()
}

func drawBolt(dc *gg.Context, cx, cy, size float64) {
	dc.SetHexColor(iconBolt)
	dc.MoveTo(cx+0.04*size, cy+0.16*size)
	dc.LineTo(cx-0.12*size, cy+0.40*size)
	dc.LineTo(cx-0.01*size, cy+0.40*size)
	dc.LineTo(cx-0.08*size, cy+0.62*size)
	dc.LineTo(cx+0.16*size, cy+0.30*size)
	dc.LineTo(cx+0.03*size, cy+0.30*size)
	dc.ClosePath()
	dc.Fill()
}

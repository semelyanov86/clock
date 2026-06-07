package render

import "time"

var ruWeekdayShortNames = map[time.Weekday]string{
	time.Monday:    "Пн",
	time.Tuesday:   "Вт",
	time.Wednesday: "Ср",
	time.Thursday:  "Чт",
	time.Friday:    "Пт",
	time.Saturday:  "Сб",
	time.Sunday:    "Вс",
}

var ruWeekdayLongNames = map[time.Weekday]string{
	time.Monday:    "Понедельник",
	time.Tuesday:   "Вторник",
	time.Wednesday: "Среда",
	time.Thursday:  "Четверг",
	time.Friday:    "Пятница",
	time.Saturday:  "Суббота",
	time.Sunday:    "Воскресенье",
}

var ruMonthNominative = map[time.Month]string{
	time.January: "Январь", time.February: "Февраль", time.March: "Март",
	time.April: "Апрель", time.May: "Май", time.June: "Июнь",
	time.July: "Июль", time.August: "Август", time.September: "Сентябрь",
	time.October: "Октябрь", time.November: "Ноябрь", time.December: "Декабрь",
}

var ruMonthGenitive = map[time.Month]string{
	time.January: "января", time.February: "февраля", time.March: "марта",
	time.April: "апреля", time.May: "мая", time.June: "июня",
	time.July: "июля", time.August: "августа", time.September: "сентября",
	time.October: "октября", time.November: "ноября", time.December: "декабря",
}

func ruWeekdayShort(t time.Time) string { return ruWeekdayShortNames[t.Weekday()] }
func ruWeekdayLong(t time.Time) string  { return ruWeekdayLongNames[t.Weekday()] }
func ruMonthNom(m time.Month) string    { return ruMonthNominative[m] }
func ruMonthGen(m time.Month) string    { return ruMonthGenitive[m] }

// mondayIndex returns the 0-based column for a weekday in a Monday-first grid.
func mondayIndex(wd time.Weekday) int {
	if wd == time.Sunday {
		return 6
	}
	return int(wd) - 1
}

// ruWeekHeaders are the Monday-first column headers for the calendar.
var ruWeekHeaders = []string{"Пн", "Вт", "Ср", "Чт", "Пт", "Сб", "Вс"}

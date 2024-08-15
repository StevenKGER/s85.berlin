package internal

import "time"

type DepartureStatus int64

const (
	RUNNING DepartureStatus = iota
	NOT_RUNNING
	CLOSING_TIME
	NO_INFORMATION
)

type DepartureInformation struct {
	Status         DepartureStatus
	Time           time.Time
	StatusMessages map[string][]string
}

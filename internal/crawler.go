package internal

import (
	"github.com/Jeffail/gabs/v2"
	"io"
	"net/http"
	"slices"
	"time"
)

func crawlInformationAboutDeparture() *DepartureInformation {
	result := &DepartureInformation{Status: NO_INFORMATION, Time: time.Now(), StatusMessages: []string{}}

	json, err := getRawDepartureInformation()
	if err != nil {
		Log.Error("parsing raw departure JSON failed", err)
		return result
	}

	for _, departure := range json.S("departures").Children() {
		if departure.S("line").S("name").Data().(string) != "S85" {
			continue
		}
		result.Status = RUNNING

		remarks := departure.S("remarks")
		for _, remark := range remarks.Children() {
			if remark.S("type").Data().(string) == "status" && (remark.S("code").Data().(string) == "text.realtime.journey.cancelled" ||
				remark.S("code").Data().(string) == "text.realtime.stop.cancelled") {
				result.Status = NOT_RUNNING
			}

			if remark.S("type").Data().(string) == "warning" {
				text := remark.S("text").Data().(string)
				if slices.Contains(result.StatusMessages, text) {
					continue
				}
				result.StatusMessages = append(result.StatusMessages, text)
			}
		}
	}

	if result.Status == NO_INFORMATION {
		result.Status = CLOSING_TIME
	}

	return result
}

func getRawDepartureInformation() (*gabs.Container, error) {
	resp, err := http.Get("https://v6.vbb.transport.rest/stops/900191001/departures" +
		"?subway=false&tram=false&bus=false&ferry=false&regional=false&express=false&duration=30")
	if err != nil {
		Log.Errorf("Error getting raw departure information: %v", err)
		return nil, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		Log.Error("Error getting raw departure information", resp.Status)
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			Log.Error("Error getting raw departure information", err)
			return nil, err
		}
		Log.Error("Error getting raw departure information", string(body))
	}

	return gabs.ParseJSONBuffer(resp.Body)
}

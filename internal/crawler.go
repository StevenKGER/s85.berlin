package internal

import (
	"fmt"
	"github.com/Jeffail/gabs/v2"
	"io"
	"net/http"
	"os"
	"slices"
	"time"
)

func CrawlInformationAboutDeparture() *DepartureInformation {
	result := &DepartureInformation{Status: NO_INFORMATION, Time: time.Now(), StatusMessages: []string{}}
	json, err := getRawDepartureInformation()
	if err != nil {
		Log.Error("parsing raw departure JSON failed", err)
		return result
	}

	Log.Infoln("looking for S85 departure information...")
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

	if result.Status == NOT_RUNNING {
		err = writeDebugToFile(result, *json)
		if err != nil {
			panic(err)
		}
	}

	Log.Infoln(result)

	return result
}

func getRawDepartureInformation() (*gabs.Container, error) {
	Log.Infoln("sending request to VBB API")
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

func writeDebugToFile(information *DepartureInformation, json gabs.Container) error {
	file, err := os.Create(fmt.Sprintf("result-%s.json", time.Now().Format("2006-01-02T15:04:05")))
	if err != nil {
		return err
	}
	defer file.Close()

	json.Set(information.Status, "crawler_status")
	json.Set(information.Time, "crawler_time")

	_, err = file.WriteString(json.StringIndent("", "    "))
	if err != nil {
		return err
	}

	return nil
}

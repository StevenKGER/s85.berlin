package internal

import (
	"fmt"
	"html/template"
	"io"
	"net/http"
	"os"
	"regexp"
	"slices"
	"time"

	"github.com/Jeffail/gabs/v2"
)

var (
	fullHtmlTagRegex    = regexp.MustCompile(`(<[a-zA-Z]+[^<]*>(?:[^<]*)</[a-zA-Z]+[^<]*>)`)
	compactHtmlTagRegex = regexp.MustCompile(`(<[a-zA-Z]+[^<]*/?>)(?:[^<]*)`)
	patternList         = []*regexp.Regexp{fullHtmlTagRegex, compactHtmlTagRegex}
)

func CrawlInformationAboutDeparture() *DepartureInformation {
	result := &DepartureInformation{Status: NO_INFORMATION, Time: time.Now(), StatusMessages: map[string][]string{}}
	json, err := getRawDepartureInformation()
	if err != nil {
		Log.Error("parsing raw departure JSON failed", err)
		return result
	}

	Log.Infoln("looking for S85 departure information...")
	var s85Departures float32
	var runningS85Departures float32
	for _, departure := range json.S("departures").Children() {
		if departure.S("line").S("name").Data().(string) != "S85" {
			continue
		}
		s85Departures += 1
		isDepatureRunning := true

		remarks := departure.S("remarks")
		for _, remark := range remarks.Children() {
			if remark.S("type").Data().(string) == "status" && (remark.S("code").Data().(string) == "text.realtime.journey.cancelled" ||
				remark.S("code").Data().(string) == "text.realtime.stop.cancelled") {
				result.Status = NOT_RUNNING
				isDepatureRunning = false
				continue
			}

			if remark.S("type").Data().(string) == "warning" {
				text := remark.S("text").Data().(string)
				sanitizedText := removeHTMLTags(text)
				germanStatusMessages := result.StatusMessages["de"]
				if slices.Contains(germanStatusMessages, sanitizedText) {
					continue
				}
				// we still escape the string, just in case
				sanitizedText = template.HTMLEscapeString(sanitizedText)
				result.StatusMessages["de"] = append(germanStatusMessages, sanitizedText)
				englishStatusMessages := result.StatusMessages["en"]
				englishText, err := translate(sanitizedText, "de", "en")
				if err != nil {
					Log.Errorln("An error occurred while translating text:", err)
				} else {
					result.StatusMessages["en"] = append(englishStatusMessages, englishText)
				}
			}
		}

		if isDepatureRunning {
			runningS85Departures += 1
		}
	}

	if runningS85Departures/s85Departures > 0.5 {
		result.Status = RUNNING
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

// removeHTMLTags is used to remove full HTML tags (including the contents they enclose)
// sometimes the S-Bahn adds a link to their Homepage in the status message we don't want to display
func removeHTMLTags(text string) string {
	for _, regex := range patternList {
		removedChars := 0
		indexes := regex.FindAllStringSubmatchIndex(text, -1)
		for _, index := range indexes {
			start, end := index[2], index[3]
			text = text[:start-removedChars] + text[end-removedChars:]
			removedChars += end - start
		}
	}

	return text
}

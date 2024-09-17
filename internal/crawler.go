package internal

import (
	"fmt"
	"html"
	"html/template"
	"io"
	"net/http"
	"net/url"
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

		isDepatureRunning, germanStatusMessages := processRemarks(departure)
		if isDepatureRunning {
			runningS85Departures += 1
		} else {
			result.Status = NOT_RUNNING
		}

		if len(germanStatusMessages) == 0 {
			continue
		}
		for _, message := range germanStatusMessages {
			if !slices.Contains(result.StatusMessages["de"], message) {
				result.StatusMessages["de"] = append(result.StatusMessages["de"], message)
			}
		}

		tripId := departure.S("tripId").Data().(string)
		englishDepatureInformation, err := getRawTripInformation(tripId, "en")
		if err != nil {
			englishStatusMessages := getAutomaticEnglishTranslation(germanStatusMessages)
			addEnglishStatusMessages(result, englishStatusMessages)
			continue
		}

		_, englishStatusMessages := processRemarks(englishDepatureInformation)
		if slices.Compare(germanStatusMessages, englishStatusMessages) != 0 {
			addEnglishStatusMessages(result, englishStatusMessages)
		} else { // content of the slices is the same - probably both German
			englishStatusMessages := getAutomaticEnglishTranslation(germanStatusMessages)
			addEnglishStatusMessages(result, englishStatusMessages)
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

func addEnglishStatusMessages(result *DepartureInformation, englishStatusMessages []string) {
	for _, message := range englishStatusMessages {
		if !slices.Contains(result.StatusMessages["en"], message) {
			result.StatusMessages["en"] = append(result.StatusMessages["en"], message)
		}
	}
}

func getAutomaticEnglishTranslation(germanStatusMessages []string) (englishStatusMessages []string) {
	for _, message := range germanStatusMessages {
		englishText, err := translate(message, "de", "en")
		if err != nil {
			Log.Errorln("An error occurred while translating text:", err)
		} else {
			englishStatusMessages = append(englishStatusMessages, englishText)
		}
	}
	return englishStatusMessages
}

func processRemarks(departure *gabs.Container) (isDepatureRunning bool, statusMessages []string) {
	isDepatureRunning = true
	remarks := departure.S("remarks")
	for _, remark := range remarks.Children() {
		if remark.S("type").Data().(string) == "status" && (remark.S("code").Data().(string) == "text.realtime.journey.cancelled" ||
			remark.S("code").Data().(string) == "text.realtime.stop.cancelled") {
			isDepatureRunning = false
			continue
		}

		if remark.S("type").Data().(string) == "warning" {
			text := remark.S("text").Data().(string)
			sanitizedText := removeHTMLTags(text)
			if slices.Contains(statusMessages, sanitizedText) {
				continue
			}

			// convert < and >
			sanitizedText = html.UnescapeString(sanitizedText)
			// we still escape the string, just in case
			sanitizedText = template.HTMLEscapeString(sanitizedText)
			statusMessages = append(statusMessages, sanitizedText)
		}
	}

	return isDepatureRunning, statusMessages
}

func getRawDepartureInformation() (*gabs.Container, error) {
	Log.Infoln("sending station depature request to VBB API")
	resp, err := httpClient.Get("https://v6.vbb.transport.rest/stops/900191001/departures" +
		"?subway=false&tram=false&bus=false&ferry=false&regional=false&express=false" +
		"&duration=30&language=de")
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

func getRawTripInformation(tripId string, language string) (*gabs.Container, error) {
	Log.Infof("sending detail request to VBB API for trip %s\n", tripId)
	resp, err := httpClient.Get(
		fmt.Sprintf(
			"https://v6.vbb.transport.rest/trips/%s?stopovers=false&remarks=true&polyline=false&language=%s",
			url.PathEscape(tripId), language,
		),
	)
	if err != nil {
		Log.Errorf("Error getting raw trip information: %v", err)
		return nil, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		Log.Error("Error getting raw trip information", resp.Status)
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			Log.Error("Error getting raw trip information", err)
			return nil, err
		}
		Log.Error("Error getting raw trip information", string(body))
	}

	container, err := gabs.ParseJSONBuffer(resp.Body)
	if err != nil {
		return nil, err
	}

	return container.S("trip"), nil
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

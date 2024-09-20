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
	"strings"
	"time"

	"github.com/Jeffail/gabs/v2"
)

var (
	stationRangeRegex   = regexp.MustCompile(`\D\.\s*(\(.*\))$`)
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
		fetchEnglishStatusMessages(tripId, germanStatusMessages, result)
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

func fetchEnglishStatusMessages(tripId string, germanStatusMessages []string, result *DepartureInformation) {
	englishDepatureInformation, err := getRawTripInformation(tripId, "en")
	if err != nil {
		englishStatusMessages := getAutomaticEnglishTranslation(germanStatusMessages)
		appendEnglishStatusMessages(result, englishStatusMessages)
		return
	}

	_, fetchedEnglishStatusMessages := processRemarks(englishDepatureInformation)
	var messagesToTranslate []string
	var englishStatusMessages []string
	for _, message := range fetchedEnglishStatusMessages {
		if slices.Contains(germanStatusMessages, message) { // S-Bahn didn't provide an English translation for this message
			messagesToTranslate = append(messagesToTranslate, message)
		} else {
			englishStatusMessages = append(englishStatusMessages, message)
		}
	}
	if len(messagesToTranslate) == 0 {
		appendEnglishStatusMessages(result, fetchedEnglishStatusMessages)
	} else {
		englishStatusMessages := append(englishStatusMessages, getAutomaticEnglishTranslation(messagesToTranslate)...)
		appendEnglishStatusMessages(result, englishStatusMessages)
	}
}

func appendEnglishStatusMessages(result *DepartureInformation, englishStatusMessages []string) {
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
			sanitizedText := sanitizeStatusMessage(text)
			if slices.Contains(statusMessages, sanitizedText) {
				continue
			}
			statusMessages = append(statusMessages, sanitizedText)
		}
	}

	return isDepatureRunning, statusMessages
}

func sanitizeStatusMessage(text string) string {
	sanitizedText := removeHTMLTags(text)
	// convert < and >
	sanitizedText = html.UnescapeString(sanitizedText)
	// we still keep escaping the string, just in case
	sanitizedText = template.HTMLEscapeString(sanitizedText)
	sanitizedText = strings.ReplaceAll(sanitizedText, "\n", "")
	stationRangeMatch := stationRangeRegex.FindStringSubmatchIndex(sanitizedText)
	// [start of full match, end of full match, start of group match, end of group match]
	if len(stationRangeMatch) == 4 {
		sanitizedText = sanitizedText[:stationRangeMatch[2]]
	}
	sanitizedText = strings.TrimSpace(sanitizedText)
	return sanitizedText
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

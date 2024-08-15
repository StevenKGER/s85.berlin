package internal

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"slices"

	"github.com/Jeffail/gabs/v2"
)

var languages []string

type Languages struct {
	Languages []Language `json:"languages"`
}

type Language struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

func translate(text string, sourceLanguage string, targetLanguage string) (string, error) {
	if len(languages) == 0 {
		if err := fetchAvailableLanguages(); err != nil {
			return "", err
		}
	}

	if !slices.Contains(languages, sourceLanguage) || !slices.Contains(languages, targetLanguage) {
		return "", errors.New("language is not available")
	}

	encodedText := url.QueryEscape(text)
	resp, err := httpClient.Get(fmt.Sprintf("https://lingva.ml/api/v1/%s/%s/%s", sourceLanguage, targetLanguage, encodedText))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	content, err := gabs.ParseJSONBuffer(resp.Body)
	if err != nil {
		return "", err
	}
	if content.Exists("translation") {
		return content.S("translation").Data().(string), nil
	}
	if content.Exists("error") {
		return "", errors.New(content.S("error").Data().(string))
	}
	return "", errors.New("translation failed")
}

func fetchAvailableLanguages() error {
	resp, err := httpClient.Get("https://lingva.ml/api/v1/languages")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	var languageResponse Languages
	if err := json.Unmarshal(body, &languageResponse); err != nil {
		return err
	}
	languages = nil
	for _, language := range languageResponse.Languages {
		if language.Code == "auto" {
			continue
		}
		languages = append(languages, language.Code)
	}
	return nil
}

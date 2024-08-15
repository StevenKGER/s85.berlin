package main

import (
	"embed"
	"html/template"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/StevenKGER/s85.berlin/internal"
	"github.com/kataras/i18n"
	"github.com/sirupsen/logrus"
)

type TemplateDetails struct {
	Status             internal.DepartureStatus
	DetailText         template.HTML
	OriginalDetailText template.HTML
	Time               string
}

var (
	lock        = sync.RWMutex{}
	information = &internal.DepartureInformation{
		Status:         internal.NO_INFORMATION,
		Time:           time.Now(),
		StatusMessages: map[string][]string{},
	}
	//go:embed index.html
	indexFS       embed.FS
	indexTemplate = template.Must(template.New("index.html").Funcs(template.FuncMap{"t": i18n.Tr}).ParseFS(indexFS, "*"))

	//go:embed i18n/*
	i18nFS    embed.FS
	translate *i18n.I18n
)

func main() {
	loader, err := i18n.FS(i18nFS, "./i18n/*.json")
	if err != nil {
		internal.Log.Fatalln("Error while loading i18n:", err)
	}
	translate, err = i18n.New(loader, "en", "de")
	if err != nil {
		internal.Log.Fatalln("Error while loading i18n:", err)
	}

	go func() {
		for {
			lock.Lock()
			information = internal.CrawlInformationAboutDeparture()
			lock.Unlock()
			time.Sleep(1 * time.Minute)
		}
	}()

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		locale := translate.GetLocale(r)
		language := locale.Tag().String()
		var originalDetail string

		lock.RLock()
		detail := strings.Join(information.StatusMessages[language], "<br>")
		if language != "de" {
			originalDetail = strings.Join(information.StatusMessages["de"], "<br>")
		}
		data := TemplateDetails{
			Status:             information.Status,
			DetailText:         template.HTML(detail),
			OriginalDetailText: template.HTML(originalDetail),
			Time:               information.Time.Format("2006-01-02 15:04:05"),
		}
		indexTemplate.Funcs(template.FuncMap{
			"t": locale.GetMessage,
		})
		err = indexTemplate.Execute(w, data)
		lock.RUnlock()
		if err != nil {
			internal.Log.Errorln("Error while writing a response using the index template", err)
		}

		internal.Log.WithFields(logrus.Fields{
			"uri":    r.RequestURI,
			"method": r.Method,
			"header": r.Header,
			"ip":     r.RemoteAddr,
		}).Info("request completed")
	})

	panic(http.ListenAndServe(":4269", nil))
}

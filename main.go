package main

import (
	"embed"
	"github.com/StevenKGER/s85.berlin/internal"
	"github.com/kataras/i18n"
	"github.com/sirupsen/logrus"
	"html/template"
	"net/http"
	"strings"
	"sync"
	"time"
)

type TemplateDetails struct {
	Title      string
	Status     internal.DepartureStatus
	DetailText template.HTML
	Time       string
}

var (
	lock        = sync.RWMutex{}
	information = &internal.DepartureInformation{
		Status:         internal.NO_INFORMATION,
		Time:           time.Now(),
		StatusMessages: []string{},
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
		translateFunc := translate.GetLocale(r).GetMessage

		lock.RLock()
		var detail string
		if information.Status == internal.NOT_RUNNING {
			detail = strings.Join(information.StatusMessages, "<br>")
		}
		data := TemplateDetails{
			Title:      "Fährt die S85?",
			Status:     information.Status,
			DetailText: template.HTML(detail),
			Time:       information.Time.Format("2006-01-02 15:04:05"),
		}
		indexTemplate.Funcs(template.FuncMap{
			"t": translateFunc,
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

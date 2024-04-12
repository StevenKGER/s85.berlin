package main

import (
	"github.com/StevenKGER/s85.berlin/internal"
	"github.com/sirupsen/logrus"
	"html/template"
	"net/http"
	"strings"
	"sync"
	"time"
)

type TemplateDetails struct {
	Title      string
	StatusText string
	DetailText string
	Time       string
}

var (
	lock        = sync.RWMutex{}
	information = &internal.DepartureInformation{
		Status:         internal.NO_INFORMATION,
		Time:           time.Now(),
		StatusMessages: []string{},
	}
	indexTemplate = template.Must(template.ParseFiles("index.html"))
)

func main() {
	go func() {
		for {
			lock.Lock()
			information = internal.CrawlInformationAboutDeparture()
			lock.Unlock()
			time.Sleep(1 * time.Minute)
		}
	}()

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		lock.RLock()
		var status, detail string
		if information.Status == internal.NO_INFORMATION {
			status = "Die S85 fährt vielleicht, vielleicht aber auch nicht."
			detail = "Etwas hat beim Abruf der Daten nicht funktioniert. Versuche es in einer Minute erneut."
		} else if information.Status == internal.CLOSING_TIME {
			status = "Die S85 schläft gerade."
			detail = "Betriebsschluss! Komm morgen wieder."
		} else if information.Status == internal.RUNNING {
			status = "Die S85 fährt."
			detail = "Glückwunsch und gute Fahrt!"
		} else if information.Status == internal.NOT_RUNNING {
			status = "Die S85 fährt nicht."
			detail = "Ohje.<br>" + strings.Join(information.StatusMessages, "<br>")
		}
		data := TemplateDetails{
			Title:      "Fährt die S85?",
			StatusText: status,
			DetailText: detail,
			Time:       information.Time.Format("2006-01-02 15:04:05"),
		}
		lock.RUnlock()
		err := indexTemplate.Execute(w, data)
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

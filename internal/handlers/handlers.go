package handlers

import (
	"html/template"
	"log"
	"net/http"
	"time"
	"sort"
	"strings"

	"github.com/base58btc/btcpp-web/external/getters"
	"github.com/base58btc/btcpp-web/internal/types"
)

func getSessionKey(p string, r *http.Request) (string, bool) {
	ok := r.URL.Query().Has(p)
	key := r.URL.Query().Get(p)
	return key, ok
}

type HomePage struct {
	Talks      talkTime
	RoundRobins []*Session
	Sessions    []*Session
	Saturday    []sessionTime
	Sunday      []sessionTime
	GoogleKey   string
}

func Home(w http.ResponseWriter, r *http.Request, ctx *types.AppContext) {
	// Parse the template file
	tmpl, err := template.ParseFiles("templates/index.tmpl", "templates/nav.tmpl", "templates/session.tmpl")
	if err != nil {
		log.Fatal(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Define the data to be rendered in the template
	var talks talkTime
	talks, err = getters.ListTalks(ctx.Notion)
	if err != nil {
		log.Fatal(w, err.Error(), http.StatusInternalServerError)
		return
	}

	sort.Sort(talks)

	var sessions []*Session
	var roundRobins []*Session
	for _, talk := range talks {
		if talk.Sched == nil {
			continue
		}
		session := TalkToSession(talk)

		if session.Type == "round-robin" {
			roundRobins = append(roundRobins, session)
			continue
		}

		sessions = append(sessions, session)
	}

	// Render the template with the data
	saturday, err := listSaturdaySessions(talks)
	if err != nil {
		log.Fatal(w, err.Error(), http.StatusInternalServerError)
		return
	}

	sunday, err := listSundaySessions(talks)
	if err != nil {
		log.Fatal(w, err.Error(), http.StatusInternalServerError)
		return
	}

	err = tmpl.ExecuteTemplate(w, "index.tmpl", &HomePage{
		Talks: talks,
		Sessions: sessions,
		Saturday: saturday,
		Sunday: sunday,
		RoundRobins: roundRobins,
		GoogleKey: ctx.Env.Google.Key,
	})
	if err != nil {
		log.Fatal(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

type Session struct {
	Name string
	Speaker string
	Company string
	Twitter string
	Photo string
	Sched *types.Times
	StartTime string
	Len     string
	DayTag  string
	Type    string
	Venue   string
	AnchorTag string
}

func TalkToSession(talk *types.Talk) *Session {
	sesh := &Session{
		Name: talk.Name,
		Speaker: talk.BadgeName,
		Twitter: talk.Twitter,
		Company: talk.Company,
		Photo: talk.Photo,
		Sched: talk.Sched,
		Type: talk.Type,
		Venue: strings.ToUpper(talk.Venue),
		AnchorTag: talk.AnchorTag,
	}

	if talk.Sched != nil {
		sesh.Len = talk.Sched.LenStr()
		sesh.StartTime = talk.Sched.StartTime()
		sesh.DayTag = talk.Sched.Day()
	}

	/* Hard over-ride for the special days */
	if sesh.Type == "round-robin" {
		sesh.Len = "1h"
	}

	return sesh
}

type SchedulePage struct {
	Talks []*types.Talk
	Sessions []talkTime
}

type talkTime []*types.Talk
type sessionTime []*Session

func (p talkTime) Len() int {
	return len(p)
}

func (p talkTime) Less(i, j int) bool {
	if p[i].Sched == nil {
		return true
	}
	if p[j].Sched == nil {
		return false
	}

	/* Sort by time first */
	if p[i].Sched.Start != p[j].Sched.Start {
		return p[i].Sched.Start.Before(p[j].Sched.Start)
	}

	/* Then we sort by room */
	return p[i].VenueValue() < p[j].VenueValue()
}

func (p talkTime) Swap(i, j int) {
	p[i], p[j] = p[j], p[i]
}

func listCutoffs() ([6]time.Time, error) {
	var cutoffs [6]time.Time

	cst, err := time.LoadLocation("America/Chicago")
	if err != nil {
		return cutoffs, err
	}

	cutoffs[0] = time.Date(2023, time.April, 29, 10, 25, 0, 0, cst)
	cutoffs[1] = time.Date(2023, time.April, 29, 11, 55, 0, 0, cst)
	cutoffs[2] = time.Date(2023, time.April, 29, 14, 55, 0, 0, cst)
	cutoffs[3] = time.Date(2023, time.April, 29, 15, 55, 0, 0, cst)
	cutoffs[4] = time.Date(2023, time.April, 29, 16, 25, 0, 0, cst)
	cutoffs[5] = time.Date(2023, time.April, 29, 17, 25, 0, 0, cst)
	return cutoffs, nil
}

func listSundaySessions(talks talkTime) ([]sessionTime, error) {
	var cutoffs [2]time.Time

	cst, err := time.LoadLocation("America/Chicago")
	if err != nil {
		return nil, err
	}

	/* Before + After Lunch sessions */
	cutoffs[0] = time.Date(2023, time.April, 30, 11, 55, 0, 0, cst)
	cutoffs[1] = time.Date(2023, time.April, 30, 16, 55, 0, 0, cst)

	sort.Sort(talks)

	sessions := make([]sessionTime, len(cutoffs))
	for i, _:= range sessions {
		sessions[i] = make(sessionTime, 0)
	}
	for _, talk := range talks {
		if talk.DayTag != "Sunday" {
			continue
		}
		for i, cutoff := range cutoffs {
			if talk.Sched.Start.Before(cutoff) {
				session := TalkToSession(talk)
				sessions[i] = append(sessions[i], session)
				break
			}
		}
	}
	return sessions, nil
}

func listSaturdaySessions(talks talkTime) ([]sessionTime, error) {
	cutoffs, err := listCutoffs()
	if err != nil {
		return nil, err
	}

	sort.Sort(talks)

	sessions := make([]sessionTime, len(cutoffs))
	for i, _:= range sessions {
		sessions[i] = make(sessionTime, 0)
	}
	for _, talk := range talks {
		if talk.DayTag != "Saturday" {
			continue
		}
		for i, cutoff := range cutoffs {
			if talk.Sched.Start.Before(cutoff) {
				session := TalkToSession(talk)
				sessions[i] = append(sessions[i], session)
				break
			}
		}
	}
	return sessions, nil
}

func listSaturdayTalks(talks talkTime) ([]talkTime, error) {
	cutoffs, err := listCutoffs()
	if err != nil {
		return nil, err
	}
	saturdays := make([]talkTime, len(cutoffs))
	for i, _:= range saturdays {
		saturdays[i] = make(talkTime, 0)
	}

	sort.Sort(talks)
	for _, talk := range talks {
		if talk.DayTag != "Saturday" {
			continue
		}
		for i, cutoff := range cutoffs {
			if talk.Sched.Start.Before(cutoff) {
				saturdays[i] = append(saturdays[i], talk)
				break
			}
		}
	}
	return saturdays, nil
}

func Talks(w http.ResponseWriter, r *http.Request, ctx *types.AppContext) {
	// Parse the template file
	tmpl, err := template.ParseFiles("templates/sched.tmpl",
		"templates/sched_desc.tmpl",
		"templates/nav.tmpl")
	if err != nil {
		log.Fatal(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Define the data to be rendered in the template
	var talks talkTime
	talks, err = getters.ListTalks(ctx.Notion)
	if err != nil {
		log.Fatal(w, err.Error(), http.StatusInternalServerError)
		return
	}

	sort.Sort(talks)

	// Render the template with the data
	err = tmpl.ExecuteTemplate(w, "sched.tmpl",
	&SchedulePage{
		Talks: talks,
	})
	if err != nil {
		log.Fatal(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func Styles(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Content-Type", "text/css")
	http.ServeFile(w, r, "static/css/styles.css")
}

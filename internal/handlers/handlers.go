package handlers

import (
	"context"
	"html/template"
	"fmt"
	"io/ioutil"
	"net/http"
	"log"
	"time"
	"sort"
	"strings"

	"github.com/base58btc/btcpp-web/external/getters"
	"github.com/base58btc/btcpp-web/internal/types"
	"github.com/base58btc/btcpp-web/internal/config"
	"github.com/gorilla/mux"

	qrcode "github.com/skip2/go-qrcode"
	"encoding/base64"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

func MiniCss() string {
	css, err := ioutil.ReadFile("static/css/mini.css")
	if err != nil {
		panic(err)
	}
	return string(css)
}

/* https://www.calhoun.io/intro-to-templates-p3-functions/ */
func loadTemplates(app *config.AppContext) error {

	index, err := template.ParseFiles("templates/index.tmpl", "templates/nav.tmpl", "templates/session.tmpl")
	if err != nil {
		return err
	}
	app.TemplateCache["index.tmpl"] = index

	// Parse the template file
	sched, err := template.ParseFiles("templates/sched.tmpl",
		"templates/sched_desc.tmpl",
		"templates/nav.tmpl")
	if err != nil {
		return err
	}
	app.TemplateCache["sched.tmpl"] = sched

	ticket, err := template.New("ticket.tmpl").Funcs(template.FuncMap{
		"safesrc": func(s string) template.HTMLAttr {
			return template.HTMLAttr(fmt.Sprintf(`src="%s"`, s))
		},
		"css": func(s string) template.HTML {
			return template.HTML(fmt.Sprintf(`<style>%s</style>`, s))
		},
	}).ParseFiles("templates/emails/ticket.tmpl")
	if err != nil {
		return err
	}
	app.TemplateCache["ticket.tmpl"] = ticket

	register, err := template.New("register.tmpl").Funcs(template.FuncMap{
		"css": func(s string) template.HTML {
			return template.HTML(fmt.Sprintf(`<style>%s</style>`, s))
		},
	}).ParseFiles("templates/emails/register.tmpl")
	if err != nil {
		return err
	}
	app.TemplateCache["register"] = register

	checkin, err := template.ParseFiles("templates/checkin.tmpl", "templates/nav.tmpl")
	if err != nil {
		return err
	}
	app.TemplateCache["checkin.tmpl"] = checkin

	return nil
}

// Routes sets up the routes for the application
func Routes(app *config.AppContext) (http.Handler, error) {
	// Create a file server to serve static files from the "static" directory
	fs := http.FileServer(http.Dir("static"))

	r := mux.NewRouter()

	// Set up the routes, we'll have one page per course
	r.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		Home(w, r, app)
	}).Methods("GET")
	r.HandleFunc("/talks", func(w http.ResponseWriter, r *http.Request) {
		Talks(w, r, app)
	}).Methods("GET")
	r.HandleFunc("/check-in/{ticket}", func(w http.ResponseWriter, r *http.Request) {
		CheckIn(w, r, app)
	}).Methods("GET", "POST")

	r.HandleFunc("/welcome-email", func(w http.ResponseWriter, r *http.Request) {
		TicketCheck(w, r, app)
	}).Methods("GET")
	r.HandleFunc("/ticket/{ticket}", func(w http.ResponseWriter, r *http.Request) {
		Ticket(w, r, app)
	}).Methods("GET")
	r.HandleFunc("/get-pdf", func(w http.ResponseWriter, r *http.Request) {
		MakePdf(w, r, app)
	}).Methods("GET")
	r.HandleFunc("/trial-email", func(w http.ResponseWriter, r *http.Request) {
		SendMailTest(w, r, app)
	}).Methods("GET")

	r.PathPrefix("/static/").Handler(http.StripPrefix("/static/", fs))
	err := addFaviconRoutes(r)

	if err != nil {
		return r, err
	}

	app.TemplateCache = make(map[string]*template.Template)
	err = loadTemplates(app)

	return r, err
}

func getFaviconHandler(name string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, fmt.Sprintf("static/favicon/%s", name))
	}
}

func addFaviconRoutes(r *mux.Router) error {
	files, err := ioutil.ReadDir("static/favicon/")
	if err != nil {
		return err
	}

	/* If asked for a favicon, we'll serve it up */
	for _, file := range files {
		if file.IsDir() {
			continue
		}
		r.HandleFunc(fmt.Sprintf("/%s", file.Name()), getFaviconHandler(file.Name())).Methods("GET")
	}

	return nil
}


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

func Home(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {

	// Define the data to be rendered in the template
	tmpl := ctx.TemplateCache["index.tmpl"]
	var talks talkTime
	talks, err := getters.ListTalks(ctx.Notion)
	if err != nil {
		http.Error(w, "Unable to load page, please try again later", http.StatusInternalServerError)
		ctx.Err.Printf("Unable to fetch talks from Notion!! %s\n", err.Error())
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
		http.Error(w, "Unable to load page, please try again later", http.StatusInternalServerError)
		ctx.Err.Printf("/ failed to build Saturdays! %s\n", err.Error())
		return
	}

	sunday, err := listSundaySessions(talks)
	if err != nil {
		http.Error(w, "Unable to load page, please try again later", http.StatusInternalServerError)
		ctx.Err.Printf("/ failed to build Sundays ! %s\n", err.Error())
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
		http.Error(w, "Unable to load page, please try again later", http.StatusInternalServerError)
		ctx.Err.Printf("/ ExecuteTemplate failed ! %s\n", err.Error())
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

func Talks(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	// Define the data to be rendered in the template
	var talks talkTime
	talks, err := getters.ListTalks(ctx.Notion)
	if err != nil {
		http.Error(w, "Unable to load page, please try again later", http.StatusInternalServerError)
		ctx.Err.Printf("/talks ListTalks failed! %s\n", err.Error())
		return
	}

	sort.Sort(talks)

	// Render the template with the data
	err = ctx.TemplateCache["sched.tmpl"].ExecuteTemplate(w, "sched.tmpl",
	&SchedulePage{
		Talks: talks,
	})
	if err != nil {
		http.Error(w, "Unable to load page, please try again later", http.StatusInternalServerError)
		ctx.Err.Printf("/talks ExecuteTemplate failed! %s\n", err.Error())
		return
	}
}

type EmailTmpl struct {
	URI string
	CSS string
}

type TicketTmpl struct {
	QRCodeURI string
	Domain string
	CSS string
}

func pdfGrabber(url string, res *[]byte) chromedp.Tasks {
    return chromedp.Tasks{
        emulation.SetUserAgentOverride("WebScraper 1.0"),
        chromedp.Navigate(url),
        chromedp.WaitVisible(`body`, chromedp.ByQuery),
        chromedp.ActionFunc(func(ctx context.Context) error {
            buf, _, err := page.PrintToPDF().WithPrintBackground(true).WithPreferCSSPageSize(true).WithPaperWidth(3.2).WithPaperHeight(9.25).Do(ctx)
            if err != nil {
                return err
            }
            *res = buf
            return nil
        }),
    }
}

func buildChromePdf(fromURL string) ([]byte, error) {
	taskCtx, cancel := chromedp.NewContext(
            context.Background(),
            chromedp.WithLogf(log.Printf),
        )
        defer cancel()
        var pdfBuffer []byte
	if err := chromedp.Run(taskCtx, pdfGrabber(fromURL, &pdfBuffer)); err != nil {
		return pdfBuffer, err
        }

	return pdfBuffer, nil
}

func SendMailTest(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	email := "niftynei@gmail.com"
	ticket := "testticket"

	ticketPage := fmt.Sprintf("%s/ticket/%s", ctx.Env.GetURI(), ticket)
	pdf, err := buildChromePdf(ticketPage)

	if err != nil {
		http.Error(w, "Unable to make ticket, please try again later", http.StatusInternalServerError)
		fmt.Printf("/send test mail failed ! %s\n", err.Error())
	}

	tickets := make([]*types.Ticket, 1)
	tickets[0] = &types.Ticket{
		Pdf: pdf,
		Id: ticket,
	}

	err = SendTickets(ctx, tickets, email, time.Now())

	/* Return the error */
	if err != nil {
		http.Error(w, "Unable to send ticket, please try again later", http.StatusInternalServerError)
		fmt.Printf("/send test mail failed to send! %s\n", err.Error())
	}

	return
}

func MakePdf(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	params := mux.Vars(r)
	ticket := params["ticket"]

	if ticket == "" {
		return
	}

	ticketPage := fmt.Sprintf("%s/ticket/%s", ctx.Env.GetURI(), ticket)
	buf, err := buildChromePdf(ticketPage)

	if err != nil {
		return
	}

	/* send email? */
	w.Header().Add("Content-Type", "application/pdf")
	w.Write(buf)
}

func Ticket(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	params := mux.Vars(r)
	ticket := params["ticket"]

	/* URL */
	url := fmt.Sprintf("%s/check-in/%s", ctx.Env.GetURI(), ticket)

	/* Turn the URL into a QR code! */
	qrpng, err := qrcode.Encode(url, qrcode.Medium, 256)
	qrcode := base64.StdEncoding.EncodeToString(qrpng)

	/* Turn the QR code into a data URI! */
	dataURI := fmt.Sprintf("data:image/png;base64,%s", qrcode)

	tix := &TicketTmpl{
		QRCodeURI: dataURI,
		CSS: MiniCss(),
		Domain: ctx.Env.GetDomain(),
	}

	err = ctx.TemplateCache["ticket.tmpl"].Execute(w, tix)
	if err != nil {
		http.Error(w, "Unable to load page, please try again later", http.StatusInternalServerError)
		fmt.Printf("/ticket-pdf ExecuteTemplate failed ! %s\n", err.Error())
	}
}

func TicketCheck(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	err := ctx.TemplateCache["register"].Execute(w, &EmailTmpl{
		URI: "https://btcpp.dev",
		//URI: ctx.Env.GetURI(),
		CSS: MiniCss(),
	})
	if err != nil {
		http.Error(w, "Unable to load page, please try again later", http.StatusInternalServerError)
		fmt.Printf("/conf/check-in ExecuteTemplate failed ! %s\n", err.Error())
	}
}

type CheckInPage struct {
	NeedsPin   bool
	TicketType string
	Msg        string
}

func CheckIn(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	switch r.Method {
	case http.MethodGet:
		CheckInGet(w, r, ctx)
		return
	case http.MethodPost:
		r.ParseForm()
		pin := r.Form.Get("pin")
		if pin != ctx.Env.RegistryPin {
			w.WriteHeader(http.StatusBadRequest)
			err := ctx.TemplateCache["checkin.tmpl"].ExecuteTemplate(w, "checkin.tmpl", &CheckInPage{
				NeedsPin: true,
				Msg: "Wrong pin",
			})
			if err != nil {
				http.Error(w, "Unable to load page, please try again later", http.StatusInternalServerError)
				fmt.Printf("/conf/check-in ExecuteTemplate failed ! %s\n", err.Error())
			}
			ctx.Err.Printf("/check-in wrong pin submitted! %s\n", pin)
			return
		}

		/* Set pin?? */
		ctx.Session.Put(r.Context(), "pin", pin)
		CheckInGet(w, r, ctx)
	}
}

func CheckInGet(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	/* Check for logged in */
	pin := ctx.Session.GetString(r.Context(), "pin")
	tmpl := ctx.TemplateCache["checkin.tmpl"]

	if pin == "" {
		w.Header().Set("x-missing-field", "pin")
		w.WriteHeader(http.StatusBadRequest)
		err := tmpl.ExecuteTemplate(w, "checkin.tmpl", &CheckInPage{
			NeedsPin: true,
		})
		if err != nil {
			http.Error(w, "Unable to load page, please try again later", http.StatusInternalServerError)
			ctx.Err.Printf("/conf/check-in ExecuteTemplate failed ! %s\n", err.Error())
		}
		return
	}

	if pin != ctx.Env.RegistryPin {
		w.WriteHeader(http.StatusUnauthorized)
		err := tmpl.ExecuteTemplate(w, "checkin.tmpl", &CheckInPage{
			Msg: "Wrong registration PIN",
		})
		if err != nil {
			http.Error(w, "Unable to load page, please try again later", http.StatusInternalServerError)
			ctx.Err.Printf("/conf/check-in ExecuteTemplate failed ! %s\n", err.Error())
		}
		return
	}

	params := mux.Vars(r)
	ticket := params["ticket"]

	tix_type, ok, err := getters.CheckIn(ctx.Notion, ticket)
	if !ok && err != nil {
		http.Error(w, "Unable to load page, please try again later", http.StatusInternalServerError)
		ctx.Err.Printf("Unable to check-in %s:\n", ticket, err.Error())
		return
	}

	var msg string
	if err != nil {
		msg = err.Error()
		ctx.Infos.Println("check-in problem:", msg)
	}
	err = tmpl.ExecuteTemplate(w, "checkin.tmpl", &CheckInPage{
		TicketType: tix_type,
		Msg: msg,
	})

	if err != nil {
		http.Error(w, "Unable to load page, please try again later", http.StatusInternalServerError)
		ctx.Err.Printf("/conf/check-in ExecuteTemplate failed ! %s\n", err.Error())
	}
}

func Styles(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Content-Type", "text/css")
	http.ServeFile(w, r, "static/css/styles.css")
}

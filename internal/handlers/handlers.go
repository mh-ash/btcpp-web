package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"html/template"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"
	"slices"
	"sort"
	"strings"

	"github.com/base58btc/btcpp-web/external/getters"
	"github.com/base58btc/btcpp-web/internal/types"
	"github.com/base58btc/btcpp-web/internal/config"
	"github.com/gorilla/mux"
	"github.com/gorilla/schema"

	qrcode "github.com/skip2/go-qrcode"
	"encoding/base64"

	stripe "github.com/stripe/stripe-go/v76"
	"github.com/stripe/stripe-go/v76/checkout/session"
	"github.com/stripe/stripe-go/v76/webhook"
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

	index, err := template.ParseFiles("templates/index.tmpl", "templates/main_nav.tmpl", "templates/section/about.tmpl")
	if err != nil {
		return err
	}
	app.TemplateCache["index.tmpl"] = index

	success, err := template.ParseFiles("templates/success.tmpl", "templates/main_nav.tmpl", "templates/section/about.tmpl")
	if err != nil {
		return err
	}
	app.TemplateCache["success.tmpl"] = success


	berlin, err := template.ParseFiles("templates/berlin.tmpl", "templates/conf_nav.tmpl", "templates/session.tmpl")
	if err != nil {
		return err
	}
	app.TemplateCache["berlin.tmpl"] = berlin

	talks, err := template.ParseFiles("templates/sched.tmpl",
		"templates/sched_desc.tmpl",
		"templates/conf_nav.tmpl")
	if err != nil {
		return err
	}
	app.TemplateCache["talks.tmpl"] = talks

	buenos, err := template.ParseFiles("templates/buenos.tmpl", "templates/conf_nav.tmpl", "templates/session.tmpl", "templates/btcbutton.tmpl")
	if err != nil {
		return err
	}
	app.TemplateCache["buenos.tmpl"] = buenos

	atx, err := template.ParseFiles("templates/atx.tmpl", "templates/conf_nav.tmpl", "templates/session.tmpl", "templates/btcbutton.tmpl")
	if err != nil {
		return err
	}
	app.TemplateCache["atx.tmpl"] = atx

	ticket, err := template.New("ticket.tmpl").Funcs(template.FuncMap{
		"safesrc": func(s string) template.HTMLAttr {
			return template.HTMLAttr(fmt.Sprintf(`src="%s"`, s))
		},
		"css": func(s string) template.HTML {
			return template.HTML(fmt.Sprintf(`<style type="text/css">%s</style>`, s))
		},
	}).ParseFiles("templates/emails/ticket.tmpl")
	if err != nil {
		return err
	}
	app.TemplateCache["ticket.tmpl"] = ticket

	for _, conf := range app.Confs {
		if !conf.Active {
			continue
		}

		htmlEmailTmpl := fmt.Sprintf("templates/emails/%s.tmpl", conf.Tag)
		textEmailTmpl := fmt.Sprintf("templates/emails/text-%s.tmpl", conf.Tag)

		htmlEmail, err := template.ParseFiles(htmlEmailTmpl)
		if err != nil {
			return err
		}
		app.TemplateCache["email-html-" + conf.Tag] = htmlEmail

		textEmail, err := template.ParseFiles(textEmailTmpl)
		if err != nil {
			return err
		}
		app.TemplateCache["email-text-" + conf.Tag] = textEmail
	}

	checkin, err := template.ParseFiles("templates/checkin.tmpl", "templates/main_nav.tmpl")
	if err != nil {
		return err
	}
	app.TemplateCache["checkin.tmpl"] = checkin

	return nil
}

func maybeReload(app *config.AppContext) {
	if !app.InProduction {
		err := loadTemplates(app)
		if err != nil {
			panic(err)	
		}
	}
}

func findConf(r *http.Request, app *config.AppContext) (*types.Conf, error) {
	params := mux.Vars(r)
	confTag := params["conf"]

	for _, conf := range app.Confs {
		if conf.Tag == confTag {
			return conf, nil
		}
	}

	return nil, fmt.Errorf("conf '%s' not found", confTag)
}

func findConfByRef(app *config.AppContext, confRef string) (*types.Conf) {
	for _, conf := range app.Confs {
		if conf.Ref == confRef {
			return conf
		}
	}
	return nil
}

func findTicket(app *config.AppContext, tixID string) (*types.ConfTicket, *types.Conf) {
	for _, conf := range app.Confs {
		for _, tix := range conf.Tickets {
			if tix.ID == tixID {
				return tix, conf
			}
		}
	}

	return nil, nil
}

func determineTixPrice(ctx *config.AppContext, tixSlug string) (*types.Conf, *types.ConfTicket, uint, bool, error) {
	
	tixParts := strings.Split(tixSlug, "+")
	if len(tixParts) != 3 {
		return nil, nil, 0, false, fmt.Errorf("not enough ticket parts?? needed 3. %s", tixSlug)
	}

	tix, conf := findTicket(ctx, tixParts[0])
	if tix == nil {
		return nil, nil, 0, false, fmt.Errorf("Unable to find tix %s", tixParts[0])
	}
	tixTypeOpts := []string{ "default", "local" }
	if !slices.Contains(tixTypeOpts, tixParts[1]) {
		return nil, nil, 0, false, fmt.Errorf("type %s not in list %v", tixParts[1], tixTypeOpts)
	}
	isLocal := tixParts[1] == "local"

	currencyTypeOpts := []string{ "btc", "fiat" }
	if !slices.Contains(currencyTypeOpts, tixParts[2]) {
		return nil, nil, 0, false, fmt.Errorf("type %s not in list %v", tixParts[2], currencyTypeOpts)
	}
	if tixParts[2] == "btc" {
		if isLocal {
			return conf, tix, tix.Local, true, nil
		}
		return conf, tix, tix.BTC, true, nil
	}

	if isLocal {
		return conf, tix, tix.Local, false, nil
	}
	return conf, tix, tix.USD, false, nil
}

/* Find ticket where current sold + date > inputs */
func findCurrTix(conf *types.Conf, soldCount uint) (*types.ConfTicket) {
	now := time.Now()
	/* Sort the tickets! */
	tixs := types.ConfTickets(conf.Tickets)
	sort.Sort(&tixs)
	for _, tix := range tixs {
		if tix.Expires.Start.Before(now) {
			continue
		}
		if tix.Max < soldCount {
			continue
		}
		return tix
	}

	/* No tix available! */
	return nil
}

// Routes sets up the routes for the application
func Routes(app *config.AppContext) (http.Handler, error) {
	r := mux.NewRouter()

	// Set up the routes, we'll have one page per course
	r.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		maybeReload(app)
		Home(w, r, app)
	}).Methods("GET")
	/* Legacy redirects! */
	r.HandleFunc("/berlin23", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/conf/berlin23", http.StatusSeeOther)
	}).Methods("GET")
	r.HandleFunc("/atx24", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/conf/atx24", http.StatusSeeOther)
	}).Methods("GET")
	r.HandleFunc("/ba24", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/conf/ba24", http.StatusSeeOther)
	}).Methods("GET")
	r.HandleFunc("/berlin23/talks", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/conf/berlin23/talks", http.StatusSeeOther)
	}).Methods("GET")
	r.HandleFunc("/talks", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/", http.StatusSeeOther)
	}).Methods("GET")

	r.HandleFunc("/conf/{conf}/success", func(w http.ResponseWriter, r *http.Request) {
		maybeReload(app)
		RenderConfSuccess(w, r, app)
	}).Methods("GET")
	r.HandleFunc("/conf/{conf}", func(w http.ResponseWriter, r *http.Request) {
		maybeReload(app)
		RenderConf(w, r, app)
	}).Methods("GET")
	r.HandleFunc("/conf/{conf}/talks", func(w http.ResponseWriter, r *http.Request) {
		maybeReload(app)
		RenderTalks(w, r, app)
	}).Methods("GET")
	r.HandleFunc("/tix/{tix}", func(w http.ResponseWriter, r *http.Request) {
		HandleTixSelection(w, r, app)
	})
	r.HandleFunc("/conf-reload", func(w http.ResponseWriter, r *http.Request) {
		maybeReload(app)
		ReloadConf(w, r, app)
	}).Methods("GET", "POST")
	r.HandleFunc("/check-in/{ticket}", func(w http.ResponseWriter, r *http.Request) {
		maybeReload(app)
		CheckIn(w, r, app)
	}).Methods("GET", "POST")
	r.HandleFunc("/welcome-email", func(w http.ResponseWriter, r *http.Request) {
		maybeReload(app)
		TicketCheck(w, r, app)
	}).Methods("GET")
	r.HandleFunc("/ticket/{ticket}", func(w http.ResponseWriter, r *http.Request) {
		maybeReload(app)
		Ticket(w, r, app)
	}).Methods("GET")
	r.HandleFunc("/trial-email", func(w http.ResponseWriter, r *http.Request) {
		maybeReload(app)
		SendMailTest(w, r, app)
	}).Methods("GET")

	/* Setup stripe! */
	stripe.Key = app.Env.StripeKey
	r.HandleFunc("/callback/stripe", func(w http.ResponseWriter, r *http.Request) {
		StripeCallback(w, r, app)
	}).Methods("POST")
	r.HandleFunc("/callback/opennode", func(w http.ResponseWriter, r *http.Request) {
		OpenNodeCallback(w, r, app)
	}).Methods("POST")

	// Create a file server to serve static files from the "static" directory
	fs := http.FileServer(http.Dir("static"))
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

type HomePage struct {}

type ConfPage struct {
	Conf *types.Conf
	Tix *types.ConfTicket
	Sold uint
	TixLeft uint
	Talks []*types.Talk
	Buckets map[string]sessionTime
}

type SuccessPage struct {
	Conf *types.Conf
}

func GetReloadConf(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
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
			ctx.Err.Printf("/conf/check-in ExecuteTemplate failed ! %s", err.Error())
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
			ctx.Err.Printf("/conf-reload ExecuteTemplate failed ! %s", err.Error())
		}
		return
	}

	confs, err := getters.ListConferences(ctx.Notion)
	if err != nil {
		http.Error(w, "Unable to load confereneces, please try again later", http.StatusInternalServerError)
		ctx.Err.Printf("/conf-reload conf load failed ! %s", err.Error())
	}

	/* We redirect to home on success */
	ctx.Confs = confs
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func ReloadConf(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	switch r.Method {
	case http.MethodGet:
		GetReloadConf(w, r, ctx)
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
				ctx.Err.Printf("/conf-reload ExecuteTemplate failed ! %s", err.Error())
				return
			}
			ctx.Err.Printf("/conf-reload wrong pin submitted! %s", pin)
			return
		}

		/* Set pin?? */
		ctx.Session.Put(r.Context(), "pin", pin)
		GetReloadConf(w, r, ctx)
	}
}

func RenderTalks(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	tmpl := ctx.TemplateCache["talks.tmpl"]

	conf, err := findConf(r, ctx)
	if err != nil {
		http.Error(w, "Unable to find page", 404)
		ctx.Err.Printf("Unable to find conf %s: %s", err.Error())
		return
	}

	var talks talkTime
	talks, err = getters.GetTalksFor(ctx.Notion, conf.Tag)
	if err != nil {
		http.Error(w, "Unable to load page, please try again later", http.StatusInternalServerError)
		ctx.Err.Printf("Unable to fetch talks from Notion!! %s", err.Error())
		return
	}

	sort.Sort(talks)
	err = tmpl.ExecuteTemplate(w, "sched.tmpl", &ConfPage{
		Talks: talks,
		Conf: conf,
	})
	if err != nil {
		http.Error(w, "Unable to load page, please try again later", http.StatusInternalServerError)
		ctx.Err.Printf("/%s/talks ExecuteTemplate failed ! %s", conf.Tag, err.Error())
		return
	}
}

func RenderConfSuccess(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	conf, err := findConf(r, ctx)
	if err != nil {
		http.Error(w, "Unable to find page", 404)
		ctx.Err.Printf("Unable to find conf %s: %s", err.Error())
		return
	}

	tmpl := ctx.TemplateCache["success.tmpl"]
	err = tmpl.ExecuteTemplate(w, "success.tmpl", &SuccessPage{
		Conf: conf,
	})
	if err != nil {
		http.Error(w, "Unable to load page, please try again later", http.StatusInternalServerError)
		ctx.Err.Printf("/conf/%s/success ExecuteTemplate failed ! %s", conf.Tag, err.Error())
		return
	}
}

func RenderConf(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	conf, err := findConf(r, ctx)
	if err != nil {
		http.Error(w, "Unable to find page", 404)
		ctx.Err.Printf("Unable to find conf %s: %s", err.Error())
		return
	}

	var talks talkTime
	talks, err = getters.GetTalksFor(ctx.Notion, conf.Tag)
	if err != nil {
		http.Error(w, "Unable to load page, please try again later", http.StatusInternalServerError)
		ctx.Err.Printf("Unable to fetch talks from Notion!! %s", err.Error())
		return
	}

	soldCount, err := getters.SoldTixCount(ctx.Notion, conf.Ref)
	if err != nil {
		http.Error(w, "Unable to load page, please try again later", http.StatusInternalServerError)
		ctx.Err.Printf("Unable to fetch ticket count from Notion!! %s", err.Error())
		return
	}

	buckets, err := bucketTalks(talks)
	if err != nil {
		http.Error(w, "Unable to load page, please try again later", http.StatusInternalServerError)
		ctx.Err.Printf("Unable to bucket '%s' talks from Notion!! %s", conf.Tag, err.Error())
		return
	}

	currTix := findCurrTix(conf, soldCount)
	var tixLeft uint
	if currTix == nil {
		tixLeft = 0
	} else {
		tixLeft = currTix.Max - soldCount
	}
	tmpl := ctx.TemplateCache[conf.Template]
	err = tmpl.ExecuteTemplate(w, conf.Template, &ConfPage{
		Conf: conf,
		Tix: currTix,
		Sold: soldCount,
		TixLeft: tixLeft,
		Talks: talks,
		Buckets: buckets,
	})
	if err != nil {
		http.Error(w, "Unable to load page, please try again later", http.StatusInternalServerError)
		ctx.Err.Printf("/%s ExecuteTemplate failed ! %s", conf.Tag, err.Error())
		return
	}
}

func Home(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {

	// Define the data to be rendered in the template
	tmpl := ctx.TemplateCache["index.tmpl"]

	err := tmpl.ExecuteTemplate(w, "index.tmpl", &HomePage{})
	if err != nil {
		http.Error(w, "Unable to load page, please try again later", http.StatusInternalServerError)
		ctx.Err.Printf("/ ExecuteTemplate failed ! %s", err.Error())
		return
	}
}

type Session struct {
	Name string
	Speaker string
	Company string
	Twitter string
	Website string
	Photo string
	TalkPhoto string
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
		Website: talk.Website,
		Company: talk.Company,
		Photo: talk.Photo,
		TalkPhoto: talk.Clipart,
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

func bucketTalks(talks talkTime) (map[string]sessionTime, error) {
	sort.Sort(talks)

	sessions := make(map[string]sessionTime)
	for _, talk := range talks {
		session := TalkToSession(talk)
		section, ok := sessions[talk.Section]
		if !ok {
			section = make(sessionTime, 0)
		}
		section = append(section, session)
		sessions[talk.Section] = section
	}
	return sessions, nil
}

type EmailTmpl struct {
	URI string
	CSS string
}

type TicketTmpl struct {
	QRCodeURI string
	Domain string
	CSS string
	Type string
	Conf *types.Conf
}

func SendMailTest(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	reg := &types.Registration{
		RefID: "testticket",
		Type: "volunteer",
		Email: "niftynei@gmail.com",
		ItemBought: "bitcoin++",
	}

	sendMail(w, r, ctx, reg)
}

func sendMail(w http.ResponseWriter, r *http.Request, ctx *config.AppContext, reg *types.Registration) {
	pdf, err := MakeTicketPDF(ctx, reg)

	if err != nil {
		http.Error(w, "Unable to make ticket, please try again later", http.StatusInternalServerError)
		ctx.Err.Printf("/send test mail failed ! %s", err.Error())
		return
	}

	tickets := make([]*types.Ticket, 1)
	tickets[0] = &types.Ticket{
		Pdf: pdf,
		ID: reg.RefID,
	}

	err = SendTickets(ctx, tickets, reg.ConfRef, reg.Email, time.Now())

	/* Return the error */
	if err != nil {
		http.Error(w, "Unable to send ticket, please try again later", http.StatusInternalServerError)
		ctx.Err.Printf("/send test mail failed to send! %s", err.Error())
		return
	}
}

func Ticket(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	params := mux.Vars(r)
	ticket := params["ticket"]

	tixType, _ := getSessionKey("type", r)
	confRef, _ := getSessionKey("conf", r)

	/* make it pretty */
	if tixType == "genpop" {
		tixType = "general"
	}

	conf := findConfByRef(ctx, confRef)
	if conf == nil {
		http.Error(w, "Unable to load page, please try again later", http.StatusInternalServerError)
		ctx.Err.Printf("/ticket-pdf unable to find conf! %s", confRef)
		return
	}

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
		Type: tixType,
		Conf: conf,
	}

	err = ctx.TemplateCache["ticket.tmpl"].Execute(w, tix)
	if err != nil {
		http.Error(w, "Unable to load page, please try again later", http.StatusInternalServerError)
		ctx.Infos.Printf("/ticket-pdf ExecuteTemplate failed ! %s", err.Error())
	}
}

func TicketCheck(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	confTag, _ := getSessionKey("tag", r)

	tmplTag := "email-html-" + confTag
	tmpl, ok := ctx.TemplateCache[tmplTag]	
	if !ok {
		http.Error(w, "Unable to load page, please try again later", http.StatusInternalServerError)
		ctx.Infos.Printf("/welcome-email template %s not found", tmplTag)
		return
	}
	err := tmpl.Execute(w, &EmailTmpl{
		URI: ctx.Env.GetURI(),
	})
	if err != nil {
		http.Error(w, "Unable to load page, please try again later", http.StatusInternalServerError)
		ctx.Infos.Printf("/welcome-email ExecuteTemplate failed ! %s", err.Error())
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
				ctx.Err.Printf("/conf/check-in ExecuteTemplate failed ! %s", err.Error())
				return
			}
			ctx.Err.Printf("/check-in wrong pin submitted! %s", pin)
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
			ctx.Err.Printf("/conf/check-in ExecuteTemplate failed ! %s", err.Error())
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
			ctx.Err.Printf("/conf/check-in ExecuteTemplate failed ! %s", err.Error())
		}
		return
	}

	params := mux.Vars(r)
	ticket := params["ticket"]

	tix_type, ok, err := getters.CheckIn(ctx.Notion, ticket)
	if !ok && err != nil {
		http.Error(w, "Unable to load page, please try again later", http.StatusInternalServerError)
		ctx.Err.Printf("Unable to check-in %s:", ticket, err.Error())
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
		ctx.Err.Printf("/conf/check-in ExecuteTemplate failed ! %s", err.Error())
	}
}

func ticketMatch(tickets []string, desc string) bool {
	for _, tix := range tickets {
		if strings.Contains(desc, tix) {
			return true
		}
	}

	return false
}

func computeHash(key, id string) string {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(id))
	return hex.EncodeToString(mac.Sum(nil))
}

func validHash(key, id, msgMAC string) bool {
	actual := computeHash(key, id)
	return msgMAC == actual
}

var decoder = schema.NewDecoder()

func OpenNodeCallback(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	err := r.ParseForm()
	if err != nil {
		ctx.Err.Printf("Error reading request body: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	var ev ChargeEvent
	decoder.IgnoreUnknownKeys(true)
	err = decoder.Decode(&ev, r.PostForm)
	if err != nil {
		ctx.Err.Printf("Unable to unmarshal: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	/* Check the hashed order is ok */
	if !validHash(ctx.Env.OpenNodeKey, ev.ID, ev.HashedOrder) {
		ctx.Err.Printf("Invalid request from opennode %s %s", ev.ID, ev.HashedOrder)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	/* Go get the actual event data */
	charge, err := GetCharge(ctx, ev.ID)
	if err != nil {
		ctx.Err.Printf("Unable to fetch charge", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	entry := types.Entry{
		ID:       charge.ID,
		Total:    int64(charge.FiatVal),
		Currency: "USD",
		Created:  charge.CreatedAt,
		Email:    charge.Metadata.Email,
	}

	if err != nil {
		ctx.Err.Printf("Failed to fetch charge %s", err)
		w.WriteHeader(http.StatusInternalServerError)
	}

	for i := 0; i < int(charge.Metadata.Quantity); i++ {
		item := types.Item{
			Total:    int64(charge.FiatVal / charge.Metadata.Quantity) * 100,
			Desc:     charge.Description,
		}
		entry.Items = append(entry.Items, item)
	}

	if len(entry.Items) == 0 {
		ctx.Infos.Println("No valid items bought")
		w.WriteHeader(http.StatusOK)
		return
	}

	err = getters.AddTickets(ctx.Notion, &entry, "opennode")

	if err != nil {
		ctx.Err.Printf("!!! Unable to add ticket %s: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	ctx.Infos.Println("Added ticket!", entry.ID)
	w.WriteHeader(http.StatusOK)
}

func HandleTixSelection(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	params := mux.Vars(r)
	tixSlug := params["tix"]

	if tixSlug == "" {
		http.Redirect(w, r, "/conf/berlin23", http.StatusSeeOther)
		return
	}

	conf, tix, tixPrice, processBTC, err := determineTixPrice(ctx, tixSlug)
	if err != nil {
		ctx.Err.Printf("/tix/%s unable to determine tix price: %s", tixSlug, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if !processBTC {
		StripeInit(w, r, ctx, conf, tix, tixPrice)
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
	//return OpenNodeInit(w, r, ctx, conf, tix, tixPrice)
}

func StripeInit(w http.ResponseWriter, r *http.Request, ctx *config.AppContext, conf *types.Conf, tix *types.ConfTicket, tixPrice uint) {

	domain := ctx.Env.GetURI()
	priceAsCents := int64(tixPrice * 100)
	confDesc := fmt.Sprintf("1 ticket for the %s", conf.Desc)
	metadata := make(map[string]string)
	metadata["conf-tag"] = conf.Tag
	metadata["conf-ref"] = conf.Ref
	metadata["tix-id"] = tix.ID
	if tixPrice == tix.Local {
		metadata["tix-local"] = "yes"
	}
	params := &stripe.CheckoutSessionParams{
		LineItems: []*stripe.CheckoutSessionLineItemParams{
		&stripe.CheckoutSessionLineItemParams{
			PriceData: &stripe.CheckoutSessionLineItemPriceDataParams {
				ProductData: &stripe.CheckoutSessionLineItemPriceDataProductDataParams{
					Description: stripe.String(confDesc),
					Name: stripe.String(conf.Desc),
					Metadata: metadata,
				},
				UnitAmount: stripe.Int64(priceAsCents),
				Currency: stripe.String("USD"),

			},
			Quantity: stripe.Int64(1),
		},},
		Metadata: metadata,
		Mode: stripe.String(string(stripe.CheckoutSessionModePayment)),
		SuccessURL: stripe.String(domain + "/conf/" + conf.Tag + "/success"),
		CancelURL: stripe.String(domain + "/conf/" + conf.Tag),
		AutomaticTax: &stripe.CheckoutSessionAutomaticTaxParams{Enabled: stripe.Bool(true)},
		AllowPromotionCodes: stripe.Bool(true),
	}

	s, err := session.New(params)
	if err != nil {
		ctx.Err.Printf("!!! Unable to create stripe session: %s", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, s.URL, http.StatusSeeOther)
}

func StripeCallback(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	const MaxBodyBytes = int64(65536)
	r.Body = http.MaxBytesReader(w, r.Body, MaxBodyBytes)
	payload, err := ioutil.ReadAll(r.Body)
	if err != nil {
		ctx.Err.Printf("Error reading request body: %v", err)
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}

	event, err := webhook.ConstructEvent(payload, r.Header.Get("Stripe-Signature"), ctx.Env.StripeEndpointSec)

	if err != nil {
		ctx.Err.Println("Error verifying webhook sig", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	switch event.Type {
	case "checkout.session.completed":
		var checkout stripe.CheckoutSession
		err := json.Unmarshal(event.Data.Raw, &checkout)
		if err != nil {
			ctx.Err.Printf("Error parsing webhook JSON: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		confRef, ok := checkout.Metadata["conf-ref"]
		if !ok {
			ctx.Infos.Println("No conf-ref present")
			w.WriteHeader(http.StatusOK)
			return
		}
		conf := findConfByRef(ctx, confRef)
		if conf == nil {
			ctx.Err.Println("Couldn't find conf %s", confRef)
			w.WriteHeader(http.StatusOK)
			return
		}

		entry := types.Entry{
			ID:       checkout.ID,
			ConfRef:  conf.Ref,
			Total:    checkout.AmountTotal,
			Currency: string(checkout.Currency),
			Created:  time.Unix(checkout.Created, 0).UTC(),
			Email:    checkout.CustomerDetails.Email,
		}

		itemParams := &stripe.CheckoutSessionListLineItemsParams{
			Session: stripe.String(checkout.ID),
		}

		_, isLocal := checkout.Metadata["tix-local"]
		var tixType string
		if isLocal {
			tixType = "local"
		} else {
			tixType = "genpop"
		}
		items := session.ListLineItems(itemParams)
		for items.Next() {
			si := items.LineItem()
			var i int64
			for i = 0; i < si.Quantity; i++ {
				item := types.Item{
					Total:    si.AmountTotal,
					Desc:     si.Description,
					Type:     tixType,
				}
				entry.Items = append(entry.Items, item)
			}
		}

		if err := items.Err(); err != nil {
			ctx.Err.Println(err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		if len(entry.Items) == 0 {
			ctx.Infos.Println("No valid items bought")
			w.WriteHeader(http.StatusOK)
			return
		}

		err = getters.AddTickets(ctx.Notion, &entry, "stripe")

		if err != nil {
			ctx.Err.Printf("!!! Unable to add ticket %s: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		ctx.Infos.Printf("Added %d tickets!!", len(entry.Items))
	default:
		ctx.Infos.Printf("Unhandled event type: %s", event.Type)
	}

	w.WriteHeader(http.StatusOK)
}

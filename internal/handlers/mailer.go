package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
	"io"
	"io/ioutil"
	"bytes"
	"strconv"

	"github.com/base58btc/btcpp-web/internal/config"
	"github.com/base58btc/btcpp-web/internal/types"
	"github.com/base58btc/btcpp-web/external/getters"
	mailer "github.com/base58btc/mailer/mail"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

var rezziesSent map[string]*types.Registration

func CheckForNewMails(ctx *config.AppContext) {

	if rezziesSent == nil {
		rezziesSent = make(map[string]*types.Registration)
	}

	var success, fails int
	tickets := []string { "bitcoin++ atx", "btcpp" }
	rezzies, err := getters.FetchBtcppRegistrations(tickets, ctx)
	if err != nil {
		ctx.Err.Println(err)
		return
	}

	ctx.Infos.Printf("Fetched %d registrations\n", len(rezzies))

	for _, rez := range rezzies {
		/* check local list (has sent already?) gets lost on restart */
		_, has := rezziesSent[rez.RefID]
		if has {
			continue
		}

		err = SendMail(ctx, rez)
		if err != nil {
			ctx.Err.Printf("Unable to send mail: %s", err.Error())
			fails++
		} else {
			rezziesSent[rez.RefID] = rez
			success++
		}
	}
	ctx.Infos.Printf("Of %d, sent %d mails, %d failed", success + fails, success, fails)
}

func pdfGrabber(url string, res *[]byte) chromedp.Tasks {
    return chromedp.Tasks{
        emulation.SetUserAgentOverride("WebScraper 1.0"),
        chromedp.Navigate(url),
        chromedp.WaitVisible(`body`, chromedp.ByQuery),
        chromedp.ActionFunc(func(ctx context.Context) error {
            buf, _, err := page.PrintToPDF().WithPrintBackground(true).WithPreferCSSPageSize(true).WithPaperWidth(3.2).WithPaperHeight(10.0).Do(ctx)
            if err != nil {
                return err
            }
            *res = buf
            return nil
        }),
    }
}

func buildChromePdf(ctx *config.AppContext, fromURL string) ([]byte, error) {
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("allow-insecure-localhost", true),
		chromedp.Flag("ignore-certificate-errors", true),
		chromedp.Flag("accept-insecure-certs", true),
	)

	allocCtx, cancel := chromedp.NewExecAllocator(context.Background(), opts...)
	defer cancel()

	taskCtx, cancel := chromedp.NewContext(
            allocCtx,
            chromedp.WithLogf(ctx.Infos.Printf),
        )
        defer cancel()
        var pdfBuffer []byte
	if err := chromedp.Run(taskCtx, pdfGrabber(fromURL, &pdfBuffer)); err != nil {
		return pdfBuffer, err
        }

	return pdfBuffer, nil
}

func MakeTicketPDF(ctx *config.AppContext, rez *types.Registration) ([]byte, error) {
	ticketPage := fmt.Sprintf("http://localhost:%s/ticket/%s?type=%s", ctx.Env.Port, rez.RefID, rez.Type)
	return buildChromePdf(ctx, ticketPage)
}

func SendMail(ctx *config.AppContext, rez *types.Registration) error {

	pdf, err := MakeTicketPDF(ctx, rez)
	if err != nil {
		return err
	}

	tickets := make([]*types.Ticket, 1)
	tickets[0] = &types.Ticket{
		Pdf: pdf,
		Id: rez.RefID,
	}

	err = SendTickets(ctx, tickets,  rez.Email, time.Now())
	return err
}


/* Send a request to our mailer to send a ticket at time */
func SendTickets(ctx *config.AppContext, tickets []*types.Ticket, email string, sendAt time.Time) error {
	/* Send the ticket email! */
	var htmlBody bytes.Buffer
	err := ctx.TemplateCache["register"].Execute(io.Writer(&htmlBody), &EmailTmpl{
		URI: ctx.Env.GetURI(),
		CSS: MiniCss(),
	})
	if err != nil {
		return err
	}

	if len(tickets) == 0 {
		return fmt.Errorf("No tickets present!")
	}

	var textBody bytes.Buffer
	err = ctx.TemplateCache["register-text"].Execute(io.Writer(&textBody), &EmailTmpl{
		URI: ctx.Env.GetURI(),
	})
	if err != nil {
		return err
	}

	var attaches mailer.AttachSet
	attaches = make([]*mailer.Attachment, len(tickets))
	for i, ticket := range tickets {
		attaches[i] = &mailer.Attachment{
			Content: ticket.Pdf,
			Type: "application/pdf",
			Name: fmt.Sprintf("btcpp23_ticket_%s.pdf", ticket.Id[:6]),
		}
	}

	ticketJob := tickets[0].Id
	/* Hack to push thru the test ticket, every time! */
	if ticketJob == "testticket" {
		ticketJob = ticketJob + strconv.Itoa(int(sendAt.UTC().Unix()))
	//} else if !ctx.Env.Prod {
	} else {
		ctx.Infos.Printf("About to send ticket to %s, but desisting, not prod!\n", email)
		return nil
	}

	ctx.Infos.Printf("Sending ticket to %s\n", email)

	title := fmt.Sprintf("[bitcoin++ Ticket #%s] You're Going! @ Austin, April 28-30", tickets[0].Id[:6])

	/* Build a mail to send */
	mail := &mailer.MailRequest{
		JobKey: fmt.Sprintf("%s-%s", "btcpp23", ticketJob),
		ToAddr: email,
		FromAddr: "hello@btcpp.dev",
		FromName: "bitcoin++ 🍰",
		Title: title,
		HTMLBody: htmlBody.String(),
		TextBody: textBody.String(),
		Attachments: attaches,
		SendAt: float64(sendAt.UTC().Unix()),
	}

	return SendMailRequest(ctx, mail)
}

func makeAuthStamp(secret string, timestamp string, r *http.Request) string {
	h := sha256.New()
	h.Write([]byte(secret))
	h.Write([]byte(timestamp))
	h.Write([]byte(r.URL.Path))
	h.Write([]byte(r.Method))
	return hex.EncodeToString(h.Sum(nil))
}

func SendMailRequest(ctx *config.AppContext, mail *mailer.MailRequest) error {
	/* Send as a PUT request w/ JSON body */
	payload, err := json.Marshal(mail)
	if err != nil {
		return err
	}

	client := &http.Client{}

	url := "http://104.131.77.55:9998/job"
	req, err := http.NewRequest(http.MethodPut, url, bytes.NewBuffer(payload))
	if err != nil {
		return err
	}

	timestamp := strconv.Itoa(int(time.Now().UTC().Unix()))
	secret := ctx.Env.MailerSecret
	authStamp := makeAuthStamp(secret, timestamp, req)

	req.Header.Set("Authorization", authStamp)
	req.Header.Set("X-Base58-Timestamp", timestamp)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	var ret mailer.ReturnVal
	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if err = json.Unmarshal(data, &ret); err != nil {
		return err
	}

	if !ret.Success {
		return fmt.Errorf("Unable to schedule mail: %s", ret.Message)
	}
	return nil
}


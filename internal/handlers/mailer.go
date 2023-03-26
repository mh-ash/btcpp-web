package handlers

import (
	"fmt"

	"github.com/base58btc/btcpp-web/internal/config"
	"github.com/sendgrid/sendgrid-go"
	"github.com/sendgrid/sendgrid-go/helpers/mail"
)

func SendTicketConfirmed(ctx *config.AppContext, email string) error {
	from := mail.NewEmail("Base58⛓🔓", "hello@base58.school")
	subject := fmt.Sprintf("[bitcoin++] Your Ticket to btcpp Austin 2023")
	to := mail.NewEmail("🐲", email)

	plainTextContent := "We got your ticket order, you're on the guest list!"
	htmlContent := "<strong>We got your ticket order, you're on the guest list!</strong>"
	message := mail.NewSingleEmail(from, subject, to, plainTextContent, htmlContent)
	client := sendgrid.NewSendClient(ctx.Env.SendGrid.Key)
	response, err := client.Send(message)
	if err == nil {
		fmt.Println(response.StatusCode)
		fmt.Println(response.Body)
		fmt.Println(response.Headers)
	}

	return err
}

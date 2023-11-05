package getters

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/base58btc/btcpp-web/internal/config"
	"github.com/base58btc/btcpp-web/internal/types"
)

const CHARGES_ENDPOINT string = "/charges"

func InitOpenNodeCheckout(ctx *config.AppContext, tixPrice uint, tix *types.ConfTicket, conf *types.Conf) (*types.OpenNodePayment, error) {

	metadata := &types.OpenNodeMetadata{
		Email: "stripe@example.com", // FIXME
		Quantity: float64(1),
		ConfRef: conf.Ref,
		TixLocal: tixPrice == tix.Local,
	}

	domain := ctx.Env.GetURI()
	onReq := &types.OpenNodeRequest{
		Amount:        float64(tixPrice),
		Description:   conf.Desc,
		Currency:      "USD",
		CallbackURL:   domain + "/callback/opennode",
		SuccessURL:    domain + "/conf/" + conf.Tag + "/success",
		AutoSettle:    false,
		TTL:           360,
		Metadata:      metadata,
		NotifEmail:    "niftynei@gmail.com", // FIXME
		CustomerEmail: "niftynei@gmail.com", // FIXME
	}

	payload, err := json.Marshal(onReq)
	if err != nil {
		return nil, err
	}

	chargesURL := ctx.Env.OpenNode.Endpoint + CHARGES_ENDPOINT
	req, _ := http.NewRequest("POST", chargesURL, bytes.NewBuffer(payload))
	req.Header.Add("Authorization", ctx.Env.OpenNode.Key)
	req.Header.Add("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("error returned from opennode %d: %s", resp.StatusCode, body)
	}

	var onresp types.OpenNodeResponse
	json.Unmarshal(body, &onresp)

	return onresp.Data, nil
}

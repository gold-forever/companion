package server

import (
	"context"
	"encoding/json"
	"errors"
)

// ErrNoAccount means the server cannot say which account the token uploads to (404, a server from
// before it): the companion shows no account.
var ErrNoAccount = errors.New("the server does not say which account this is")

// Account asks which account the token uploads to: its public name, and its BattleTag for this
// player's own eyes only.
func (c *Client) Account(ctx context.Context) (Account, error) {
	var a Account
	body, _, err := c.conditionalGet(ctx, "/api/v1/companion/account", "", maxAnswerBytes)
	var refused *RefusedError
	if errors.As(err, &refused) && refused.Status == 404 {
		return a, ErrNoAccount
	}
	if err != nil {
		return a, err
	}
	if err := json.Unmarshal(body, &a); err != nil {
		return Account{}, &unavailableError{cause: errors.New("an answer that is not the server's")}
	}
	return a, nil
}

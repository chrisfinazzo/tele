package tg

import (
	"context"
	"errors"
	"fmt"

	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"

	"github.com/sorokin-vladimir/tele/internal/telerr"
)

// The login is our own loop rather than gotd's auth.Flow, which asks for each
// value once and gives up on the first refusal: a mistyped code ended the login,
// and the screen went on taking input nobody read (#285). Here what the person
// got wrong is asked for again, with the reason, and only a refusal that is not
// theirs to fix ends it. Telegram limits guesses itself with a flood wait, which
// ends the login like any other refusal, so no limit is kept here.

// The reasons a step is asked for again, shown under the field.
const (
	reasonNumberInvalid   = "That number is not valid. Include the country code, e.g. +44..."
	reasonTooManyAttempts = "Too many login attempts for this number. Try again later."
	reasonCodeInvalid     = "That code is not right. Try again."
	reasonCodeExpired     = "That code has expired. A new one is on its way."
	reasonPasswordInvalid = "That password is not right. Try again."
)

// authorize returns the logged-in user, logging in first when there is no
// session to speak of.
//
// It stands in for gotd's auth.Client.Status, which reads every 401 as "not
// logged in": a banned account was walked through number and code only to meet
// the same ban (#254). Here the mapped kind decides. A session that turns out
// to be logged out is not logged in on either: the key in it survives the log
// out, a login would bind to it, and the account's files would pass to whoever
// logged in. The error goes up instead, and the host ends the account and
// starts the next one with no session (#297). Only a start with no session -
// the first, or the one after an account ended - leads into the login.
func (af *AuthFlow) authorize(ctx context.Context, self func(context.Context) (*tg.User, error),
	client auth.FlowClient, hadSession bool) (*tg.User, error) {
	user, err := self(ctx)
	if telerr.Of(err) != telerr.Unauthorized || hadSession {
		return user, err
	}
	if err := af.login(ctx, client); err != nil {
		return nil, err
	}
	return self(ctx)
}

// login logs in through client, asking the person through af.
func (af *AuthFlow) login(ctx context.Context, client auth.FlowClient) error {
	phone, sent, err := af.sendCode(ctx, client)
	if err != nil {
		return err
	}
	switch s := sent.(type) {
	case *tg.AuthSentCode:
		return af.signIn(ctx, client, phone, s)
	case *tg.AuthSentCodeSuccess:
		// Telegram let the session in without a code, as gotd's flow allows.
		switch a := s.Authorization.(type) {
		case *tg.AuthAuthorization:
			return nil
		case *tg.AuthAuthorizationSignUpRequired:
			_, err := af.SignUp(ctx)
			return err
		default:
			return fmt.Errorf("unexpected authorization type: %T", a)
		}
	default:
		return fmt.Errorf("unexpected sent code type: %T", sent)
	}
}

// sendCode asks for the number until Telegram accepts it and sends a code.
func (af *AuthFlow) sendCode(ctx context.Context, client auth.FlowClient) (string, tg.AuthSentCodeClass, error) {
	reason := ""
	for {
		phone, err := af.request(ctx, AuthRequest{Step: AuthStepPhone, Err: reason})
		if err != nil {
			return "", nil, fmt.Errorf("get phone: %w", err)
		}
		sent, err := client.SendCode(ctx, phone, auth.SendCodeOptions{})
		if tgerr.Is(err, "PHONE_NUMBER_INVALID") {
			reason = reasonNumberInvalid
			continue
		}
		// Telegram's limit on attempts carries no wait to count down, so the
		// number is asked for again rather than the login ended: the person can
		// try later without a restart (#254).
		if tgerr.Is(err, "PHONE_NUMBER_FLOOD", "PHONE_PASSWORD_FLOOD") {
			reason = reasonTooManyAttempts
			continue
		}
		if err != nil {
			return "", nil, fmt.Errorf("send code: %w", err)
		}
		return phone, sent, nil
	}
}

// signIn asks for the code until Telegram accepts it, sending a new one when
// the one asked about has expired.
func (af *AuthFlow) signIn(ctx context.Context, client auth.FlowClient, phone string, sent *tg.AuthSentCode) error {
	reason := ""
	for {
		code, err := af.askCode(ctx, sent, reason)
		if err != nil {
			return fmt.Errorf("get code: %w", err)
		}
		_, err = client.SignIn(ctx, phone, code, sent.PhoneCodeHash)
		var signUp *auth.SignUpRequired
		switch {
		case err == nil:
			return nil
		case errors.Is(err, auth.ErrPasswordAuthNeeded):
			return af.password(ctx, client)
		case tgerr.Is(err, "PHONE_CODE_INVALID"):
			reason = reasonCodeInvalid
		case tgerr.Is(err, "PHONE_CODE_EXPIRED"):
			again, err := client.SendCode(ctx, phone, auth.SendCodeOptions{})
			if err != nil {
				return fmt.Errorf("send code again: %w", err)
			}
			next, ok := again.(*tg.AuthSentCode)
			if !ok {
				return fmt.Errorf("unexpected sent code type: %T", again)
			}
			sent, reason = next, reasonCodeExpired
		case errors.As(err, &signUp):
			_, err := af.SignUp(ctx)
			return err
		default:
			return fmt.Errorf("sign in: %w", err)
		}
	}
}

// password asks for the 2FA password until Telegram accepts it. gotd reports a
// wrong one as its own auth.ErrPasswordInvalid rather than Telegram's error.
func (af *AuthFlow) password(ctx context.Context, client auth.FlowClient) error {
	reason := ""
	for {
		pw, err := af.request(ctx, AuthRequest{Step: AuthStepPassword, Err: reason})
		if err != nil {
			return fmt.Errorf("get password: %w", err)
		}
		_, err = client.Password(ctx, pw)
		if errors.Is(err, auth.ErrPasswordInvalid) {
			reason = reasonPasswordInvalid
			continue
		}
		if err != nil {
			return fmt.Errorf("check password: %w", err)
		}
		return nil
	}
}

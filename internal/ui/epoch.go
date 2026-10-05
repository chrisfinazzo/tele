package ui

import (
	"reflect"

	tea "charm.land/bubbletea/v2"
)

// An epoch is one account's turn on screen. When an account ends, the model
// drawing it is replaced by a new one with the next epoch, while commands the
// old model started may still be running: a history page, a profile, a media
// path from the account that ended. Every command the root model returns is
// stamped with its epoch, and a stamped message from another epoch is dropped
// on arrival, so nothing of one account lands on the next one's screen (#297).
//
// Stamping happens in one place, on the way out of the root model, because
// every command passes through it: there is no other model the program runs.

// epochMsg is a message produced for the model of one epoch.
type epochMsg struct {
	epoch uint64
	msg   tea.Msg
}

// Stamped marks a message the host sends on behalf of one account, the way a
// command's message is marked, so a late one from an ended account is dropped.
func Stamped(epoch uint64, msg tea.Msg) tea.Msg { return epochMsg{epoch: epoch, msg: msg} }

// teaPackage is where bubbletea's own messages come from.
var teaPackage = reflect.TypeOf(tea.QuitMsg{}).PkgPath()

// cmdType is the element type of a message that is a list of commands.
var cmdType = reflect.TypeOf((*tea.Cmd)(nil)).Elem()

// stamp wraps cmd so the message it produces carries epoch.
//
// Two kinds of message pass unwrapped. bubbletea's own are handled by the
// program before any model sees them and carry nothing of an account: a
// wrapped quit would never quit. And a list of commands - a batch, or the
// sequence bubbletea keeps unexported - is run by the program itself, so its
// commands are stamped one by one instead and the list keeps its type, which
// is what keeps a sequence in order.
func stamp(epoch uint64, cmd tea.Cmd) tea.Cmd {
	if cmd == nil {
		return nil
	}
	return func() tea.Msg {
		msg := cmd()
		if msg == nil {
			return nil
		}
		v := reflect.ValueOf(msg)
		if v.Kind() == reflect.Slice && v.Type().Elem() == cmdType {
			out := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
			for i := 0; i < v.Len(); i++ {
				inner, _ := v.Index(i).Interface().(tea.Cmd)
				out.Index(i).Set(reflect.ValueOf(stamp(epoch, inner)))
			}
			return out.Interface()
		}
		if v.Type().PkgPath() == teaPackage {
			return msg
		}
		return epochMsg{epoch: epoch, msg: msg}
	}
}

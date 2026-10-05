package ui

import (
	tea "charm.land/bubbletea/v2"
)

// Shell is the model the program runs. It outlives the accounts that pass
// through the process, as its host does, while the RootModel inside it is
// built for one account and replaced when that account ends (#297). Being the
// only model the program runs, it is also where every command is stamped with
// the account's epoch and every late message from an ended one is dropped.
type Shell struct {
	epoch uint64
	root  RootModel
}

// NewShell runs root as the model of the given epoch.
func NewShell(epoch uint64, root RootModel) Shell {
	return Shell{epoch: epoch, root: root}
}

func (s Shell) Init() tea.Cmd { return stamp(s.epoch, s.root.Init()) }

// Update takes a message only from the current epoch, or one no epoch marked,
// such as input from the terminal, and stamps whatever the model asks for.
func (s Shell) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if ended, ok := msg.(AccountEndedMsg); ok {
		return s.nextAccount(ended)
	}
	if em, ok := msg.(epochMsg); ok {
		if em.epoch != s.epoch {
			return s, nil
		}
		msg = em.msg
	}
	next, cmd := s.root.Update(msg)
	s.root = next.(RootModel)
	return s, stamp(s.epoch, cmd)
}

func (s Shell) View() tea.View { return s.root.View() }

// nextAccount puts the next account's model on screen in place of the one
// that ended. Nothing of the old model carries over but the terminal's size,
// which belongs to neither: what an account had on screen is the account's.
func (s Shell) nextAccount(msg AccountEndedMsg) (tea.Model, tea.Cmd) {
	width, height := s.root.width, s.root.height
	s.epoch = msg.Epoch
	s.root = msg.Root
	s.root.login.SetNotice(loginNotice(msg.Ended))
	init := s.root.Init()
	if width > 0 && height > 0 {
		next, cmd := s.root.Update(tea.WindowSizeMsg{Width: width, Height: height})
		s.root = next.(RootModel)
		init = tea.Batch(init, cmd)
	}
	return s, stamp(s.epoch, init)
}

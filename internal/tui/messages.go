package tui

import (
	"github.com/mmo-con/git-dashboard/internal/app"
	"github.com/mmo-con/git-dashboard/internal/domain"
	"github.com/mmo-con/git-dashboard/internal/inventory"
	"github.com/mmo-con/git-dashboard/internal/jobs"
)

type inventoryMsg struct {
	result inventory.Result
	// loaded is the reloaded configuration that result belongs to; nil if
	// the active configuration was used.
	loaded *app.Loaded
}

type reloadFailedMsg struct{ err error }

type eventsMsg struct{ events []jobs.Event }

type tickMsg struct{}

type handoverDoneMsg struct {
	label string
	repo  domain.RepositoryID
	store domain.StoreID
	err   error
	fatal bool
}

type spinMsg struct{}

type toastExpiredMsg struct{ seq uint64 }

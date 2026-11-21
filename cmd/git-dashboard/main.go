// Command git-dashboard is a terminal dashboard for a curated collection of
// local Git repositories.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mmo-con/git-dashboard/internal/app"
	"github.com/mmo-con/git-dashboard/internal/config"
	"github.com/mmo-con/git-dashboard/internal/git"
	"github.com/mmo-con/git-dashboard/internal/tui"
)

var version = "dev"

func main() {
	os.Exit(run())
}

func run() int {
	configFlag := flag.String("config", "", "path of the configuration file")
	showVersion := flag.Bool("version", false, "print the version and exit")
	checkConfig := flag.Bool("check", false, "validate the configuration, print the inventory, and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("git-dashboard", version)
		return 0
	}

	path, err := config.Locate(*configFlag, os.Getenv)
	if err != nil {
		fmt.Fprintln(os.Stderr, "git-dashboard:", err)
		return 2
	}
	cfg, err := config.Load(path, os.Getenv)
	if err != nil {
		fmt.Fprintln(os.Stderr, "git-dashboard: invalid configuration:", err)
		return 2
	}

	client := git.New(cfg.Git.Executable)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	_, err = client.CheckVersion(ctx)
	cancel()
	if err != nil {
		fmt.Fprintln(os.Stderr, "git-dashboard:", err)
		return 1
	}

	a := app.New(path, cfg, client)
	defer a.Close()

	if *checkConfig {
		return check(a)
	}

	model := tui.New(a)
	if _, err := tea.NewProgram(model, tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "git-dashboard:", err)
		return 1
	}
	if err := model.Err(); err != nil {
		fmt.Fprintln(os.Stderr, "git-dashboard:", err)
		return 1
	}
	return 0
}

// check prints the resolved inventory without starting the TUI.
func check(a *app.Application) int {
	if a.Config.File == "" {
		fmt.Printf("no configuration file at %s\n", a.ConfigPath)
	} else {
		fmt.Printf("configuration: %s\n", a.Config.File)
	}
	res := a.LoadInventory(context.Background())
	for _, w := range res.Warnings {
		fmt.Println("warning:", w)
	}
	for _, r := range res.Repositories {
		line := fmt.Sprintf("%-12s %-10s %-28s %s", r.Family, r.Source, r.Availability, r.Path)
		if r.EntryError != nil {
			line += " (" + r.EntryError.Error() + ")"
		}
		fmt.Println(line)
	}
	if !a.Lazygit.Available() {
		fmt.Println("warning:", a.Lazygit.Err)
	}
	for _, l := range a.Launchers {
		if l.Err != nil {
			fmt.Printf("warning: launcher %s: %v\n", l.Name, l.Err)
		}
	}
	fmt.Printf("%d repositories\n", len(res.Repositories))
	return 0
}

//go:build windows

package ui

import (
	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"

	"github.com/yanmad27/supcua-v2/app"
	"github.com/yanmad27/supcua-v2/storage"
)

// MainWindowController manages the main application window.
type MainWindowController struct {
	mainWindow      *walk.MainWindow
	appController   *app.Controller
	scriptStore     *storage.ScriptStore
	scriptListBox   *walk.ListBox
	editorContainer *walk.Composite
}

// CreateMainWindow builds and returns the main application window (hidden initially).
func CreateMainWindow(appController *app.Controller, scriptStore *storage.ScriptStore) (*walk.MainWindow, *MainWindowController, error) {
	windowController := &MainWindowController{
		appController: appController,
		scriptStore:   scriptStore,
	}

	var mainWindowInstance *walk.MainWindow

	err := (MainWindow{
		AssignTo: &mainWindowInstance,
		Title:    "supcua-v2 - Script Automation",
		MinSize:  Size{Width: 800, Height: 500},
		Size:     Size{Width: 1000, Height: 600},
		Layout:   HBox{MarginsZero: true},
		Visible:  true,
		Children: []Widget{
			ListBox{
				AssignTo: &windowController.scriptListBox,
				MinSize:  Size{Width: 200},
				MaxSize:  Size{Width: 250},
				OnCurrentIndexChanged: func() {
					windowController.onScriptSelected()
				},
			},
			Composite{
				AssignTo: &windowController.editorContainer,
				Layout:   VBox{},
			},
		},
		MenuItems: []MenuItem{
			Menu{
				Text: "&Script",
				Items: []MenuItem{
					Action{
						Text:        "&New Script",
						OnTriggered: func() { windowController.onNewScript() },
					},
					Action{
						Text:        "&Delete Script",
						OnTriggered: func() { windowController.onDeleteScript() },
					},
				},
			},
		},
	}).Create()

	if err != nil {
		return nil, nil, err
	}

	windowController.mainWindow = mainWindowInstance
	windowController.refreshScriptList()

	return mainWindowInstance, windowController, nil
}

func (windowController *MainWindowController) refreshScriptList() {
	allScripts, err := windowController.scriptStore.List()
	if err != nil {
		return
	}

	scriptNames := make([]string, len(allScripts))
	for index, script := range allScripts {
		statusPrefix := "[ ] "
		if script.Enabled {
			statusPrefix = "[x] "
		}
		scriptNames[index] = statusPrefix + script.Name
	}

	windowController.scriptListBox.SetModel(scriptNames)
}

func (windowController *MainWindowController) onScriptSelected() {
	// Will be implemented in script editor task
}

func (windowController *MainWindowController) onNewScript() {
	// Will be implemented in script editor task
}

func (windowController *MainWindowController) onDeleteScript() {
	// Will be implemented in script editor task
}

package executor

import (
	"context"
	"testing"

	"github.com/yanmad27/supcua-v2/model"
)

type mockInputSimulator struct {
	executedActions []string
}

func (mockSimulator *mockInputSimulator) MoveMouse(positionX, positionY int) error {
	mockSimulator.executedActions = append(mockSimulator.executedActions, "mouse_move")
	return nil
}
func (mockSimulator *mockInputSimulator) GetCursorPos() (int, int) { return 0, 0 }
func (mockSimulator *mockInputSimulator) MouseLeftDown() {
	mockSimulator.executedActions = append(mockSimulator.executedActions, "mouse_left_down")
}
func (mockSimulator *mockInputSimulator) MouseLeftUp() {
	mockSimulator.executedActions = append(mockSimulator.executedActions, "mouse_left_up")
}
func (mockSimulator *mockInputSimulator) MouseRightDown() {
	mockSimulator.executedActions = append(mockSimulator.executedActions, "mouse_right_down")
}
func (mockSimulator *mockInputSimulator) MouseRightUp() {
	mockSimulator.executedActions = append(mockSimulator.executedActions, "mouse_right_up")
}
func (mockSimulator *mockInputSimulator) KeyDown(keyName string) error {
	mockSimulator.executedActions = append(mockSimulator.executedActions, "key_down:"+keyName)
	return nil
}
func (mockSimulator *mockInputSimulator) KeyUp(keyName string) error {
	mockSimulator.executedActions = append(mockSimulator.executedActions, "key_up:"+keyName)
	return nil
}
func (mockSimulator *mockInputSimulator) LockMouse()      {}
func (mockSimulator *mockInputSimulator) UnlockMouse()    {}
func (mockSimulator *mockInputSimulator) LockKeyboard()   {}
func (mockSimulator *mockInputSimulator) UnlockKeyboard() {}
func (mockSimulator *mockInputSimulator) UnlockAll()      {}

func TestExecuteBasicActions(t *testing.T) {
	mockSimulator := &mockInputSimulator{}
	scriptExecutor := NewExecutor(mockSimulator, nil, 10)

	script := model.Script{
		ID:      "test-1",
		RunMode: model.RunModeOnce,
		Actions: []model.Action{
			{Type: model.ActionMouseMove, X: 100, Y: 200},
			{Type: model.ActionMouseLeftDown},
			{Type: model.ActionMouseLeftUp},
			{Type: model.ActionKeyDown, Key: "a"},
			{Type: model.ActionKeyUp, Key: "a"},
		},
	}

	ctx := context.Background()
	err := scriptExecutor.Execute(ctx, script)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedActions := []string{"mouse_move", "mouse_left_down", "mouse_left_up", "key_down:a", "key_up:a"}
	if len(mockSimulator.executedActions) != len(expectedActions) {
		t.Fatalf("expected %d actions, got %d: %v", len(expectedActions), len(mockSimulator.executedActions), mockSimulator.executedActions)
	}
	for actionIndex, expectedAction := range expectedActions {
		if mockSimulator.executedActions[actionIndex] != expectedAction {
			t.Errorf("action %d: expected %s, got %s", actionIndex, expectedAction, mockSimulator.executedActions[actionIndex])
		}
	}
}

func TestExecuteLoop(t *testing.T) {
	mockSimulator := &mockInputSimulator{}
	scriptExecutor := NewExecutor(mockSimulator, nil, 10)

	script := model.Script{
		ID:      "test-loop",
		RunMode: model.RunModeOnce,
		Actions: []model.Action{
			{
				Type:      model.ActionLoop,
				LoopCount: 3,
				Actions: []model.Action{
					{Type: model.ActionMouseLeftDown},
					{Type: model.ActionMouseLeftUp},
				},
			},
		},
	}

	ctx := context.Background()
	err := scriptExecutor.Execute(ctx, script)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 3 iterations * 2 actions = 6
	if len(mockSimulator.executedActions) != 6 {
		t.Errorf("expected 6 actions, got %d: %v", len(mockSimulator.executedActions), mockSimulator.executedActions)
	}
}

func TestExecuteCancelledContext(t *testing.T) {
	mockSimulator := &mockInputSimulator{}
	scriptExecutor := NewExecutor(mockSimulator, nil, 10)

	script := model.Script{
		ID:      "test-cancel",
		RunMode: model.RunModeOnce,
		Actions: []model.Action{
			{Type: model.ActionDelay, DelayMs: 5000},
			{Type: model.ActionMouseLeftDown},
		},
	}

	ctx, cancelFunction := context.WithCancel(context.Background())
	cancelFunction() // cancel immediately

	err := scriptExecutor.Execute(ctx, script)
	if err == nil {
		t.Error("expected context cancelled error")
	}
}

func TestExecuteInfiniteMode(t *testing.T) {
	mockSimulator := &mockInputSimulator{}
	scriptExecutor := NewExecutor(mockSimulator, nil, 10)

	script := model.Script{
		ID:      "test-infinite",
		RunMode: model.RunModeInfinite,
		Actions: []model.Action{
			{Type: model.ActionMouseLeftDown},
		},
	}

	ctx, cancelFunction := context.WithCancel(context.Background())

	executionDone := make(chan error, 1)
	go func() {
		executionDone <- scriptExecutor.Execute(ctx, script)
	}()

	// Let it run a few iterations then cancel
	for len(mockSimulator.executedActions) < 5 {
		// spin until enough actions
	}
	cancelFunction()

	err := <-executionDone
	if err == nil {
		t.Error("expected context cancelled error")
	}
	if len(mockSimulator.executedActions) < 5 {
		t.Errorf("expected at least 5 actions, got %d", len(mockSimulator.executedActions))
	}
}

package executor

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/yanmad27/supcua-v2/model"
)

// InputSimulatorInterface abstracts input simulation for testing.
type InputSimulatorInterface interface {
	MoveMouse(positionX, positionY int) error
	GetCursorPos() (int, int)
	MouseLeftDown()
	MouseLeftUp()
	MouseRightDown()
	MouseRightUp()
	KeyDown(keyName string) error
	KeyUp(keyName string) error
	LockMouse()
	UnlockMouse()
	LockKeyboard()
	UnlockKeyboard()
	UnlockAll()
}

// ImageDetectorInterface abstracts image detection for testing.
type ImageDetectorInterface interface {
	FindImage(imageBase64 string, searchRegion *model.Region) (foundX int, foundY int, found bool, err error)
}

// StatusCallback is called when the executor status changes.
type StatusCallback func(scriptID string, status string, errorMessage string)

// Executor runs script actions line by line.
type Executor struct {
	inputSimulator     InputSimulatorInterface
	imageDetector      ImageDetectorInterface
	keyPressDurationMs int
	foundPositionX     int
	foundPositionY     int
	storedPositionX    int
	storedPositionY    int
}

// NewExecutor creates an Executor with the given input simulator, image detector, and key press duration.
func NewExecutor(inputSimulator InputSimulatorInterface, imageDetector ImageDetectorInterface, keyPressDurationMs int) *Executor {
	return &Executor{
		inputSimulator:     inputSimulator,
		imageDetector:      imageDetector,
		keyPressDurationMs: keyPressDurationMs,
	}
}

// Execute runs a script's actions. Blocks until complete, cancelled, or error.
func (scriptExecutor *Executor) Execute(ctx context.Context, script model.Script) error {
	defer scriptExecutor.inputSimulator.UnlockAll()
	for {
		if err := scriptExecutor.executeActions(ctx, script.Actions); err != nil {
			return err
		}
		if script.RunMode != model.RunModeInfinite {
			return nil
		}
		if err := nextIteration(ctx, len(script.Actions) == 0); err != nil {
			return err
		}
	}
}

// nextIteration checks cancellation between iterations of an infinite loop and,
// when the body is empty, yields briefly so the loop does not spin a CPU core.
func nextIteration(ctx context.Context, emptyBody bool) error {
	if !emptyBody {
		return ctx.Err()
	}
	t := time.NewTimer(emptyLoopYield)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

const emptyLoopYield = time.Millisecond

// waitPollInterval is the delay between image checks in wait_for_image.
var waitPollInterval = 200 * time.Millisecond

func (scriptExecutor *Executor) executeActions(ctx context.Context, actionList []model.Action) error {
	i := 0
	for i < len(actionList) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		currentAction := actionList[i]

		if currentAction.Type == "loop_end" {
			i++
			continue
		}

		if currentAction.Type == model.ActionLoop {
			// If nested actions are stored inside the loop action, use those
			if len(currentAction.Actions) > 0 {
				if err := scriptExecutor.executeLoop(ctx, currentAction); err != nil {
					return err
				}
				i++
				continue
			}
			// Otherwise find matching loop_end in the flat list
			loopStart := i
			depth := 1
			j := i + 1
			for j < len(actionList) && depth > 0 {
				if actionList[j].Type == model.ActionLoop {
					depth++
				} else if actionList[j].Type == "loop_end" {
					depth--
				}
				j++
			}
			bodyEnd := j - 1
			if depth > 0 {
				bodyEnd = j
			}
			loopBody := actionList[loopStart+1 : bodyEnd]
			count := currentAction.LoopCount
			if count == 0 {
				for {
					if err := scriptExecutor.executeActions(ctx, loopBody); err != nil {
						return err
					}
					if err := nextIteration(ctx, len(loopBody) == 0); err != nil {
						return err
					}
				}
			} else {
				for k := 0; k < count; k++ {
					if err := scriptExecutor.executeActions(ctx, loopBody); err != nil {
						return err
					}
				}
			}
			i = j
			continue
		}

		if err := scriptExecutor.executeOneAction(ctx, currentAction); err != nil {
			log.Printf("action error: %v", err)
			return err
		}
		i++
	}
	return nil
}

func (scriptExecutor *Executor) executeOneAction(ctx context.Context, currentAction model.Action) error {
	switch currentAction.Type {
	case model.ActionMouseMove:
		return scriptExecutor.executeMouseMove(currentAction)

	case model.ActionMouseStorePos:
		x, y := scriptExecutor.inputSimulator.GetCursorPos()
		scriptExecutor.storedPositionX = x
		scriptExecutor.storedPositionY = y
		return nil

	case model.ActionMouseMoveStored:
		return scriptExecutor.inputSimulator.MoveMouse(scriptExecutor.storedPositionX, scriptExecutor.storedPositionY)

	case model.ActionMouseLeftClick:
		scriptExecutor.inputSimulator.MouseLeftDown()
		scriptExecutor.inputSimulator.MouseLeftUp()
		return nil

	case model.ActionMouseRightClick:
		scriptExecutor.inputSimulator.MouseRightDown()
		scriptExecutor.inputSimulator.MouseRightUp()
		return nil

	case model.ActionMouseLeftDouble:
		scriptExecutor.inputSimulator.MouseLeftDown()
		scriptExecutor.inputSimulator.MouseLeftUp()
		scriptExecutor.inputSimulator.MouseLeftDown()
		scriptExecutor.inputSimulator.MouseLeftUp()
		return nil

	case model.ActionMouseRightDouble:
		scriptExecutor.inputSimulator.MouseRightDown()
		scriptExecutor.inputSimulator.MouseRightUp()
		scriptExecutor.inputSimulator.MouseRightDown()
		scriptExecutor.inputSimulator.MouseRightUp()
		return nil

	case model.ActionMouseLeftDown:
		scriptExecutor.inputSimulator.MouseLeftDown()
		return nil

	case model.ActionMouseLeftUp:
		scriptExecutor.inputSimulator.MouseLeftUp()
		return nil

	case model.ActionMouseRightDown:
		scriptExecutor.inputSimulator.MouseRightDown()
		return nil

	case model.ActionMouseRightUp:
		scriptExecutor.inputSimulator.MouseRightUp()
		return nil

	case model.ActionKeyPress:
		return scriptExecutor.executeKeyPress(ctx, currentAction)

	case model.ActionKeyDown:
		return scriptExecutor.inputSimulator.KeyDown(currentAction.Key)

	case model.ActionKeyUp:
		return scriptExecutor.inputSimulator.KeyUp(currentAction.Key)

	case model.ActionDelay:
		return scriptExecutor.executeDelay(ctx, currentAction)

	case model.ActionLoop:
		return scriptExecutor.executeLoop(ctx, currentAction)

	case model.ActionWaitForImage:
		return scriptExecutor.executeWaitForImage(ctx, currentAction)

	case model.ActionIfImage:
		return scriptExecutor.executeIfImage(ctx, currentAction)

	case model.ActionLockMouse:
		scriptExecutor.inputSimulator.LockMouse()
		return nil

	case model.ActionUnlockMouse:
		scriptExecutor.inputSimulator.UnlockMouse()
		return nil

	case model.ActionLockKeyboard:
		scriptExecutor.inputSimulator.LockKeyboard()
		return nil

	case model.ActionUnlockKeyboard:
		scriptExecutor.inputSimulator.UnlockKeyboard()
		return nil

	default:
		return fmt.Errorf("unknown action type: %s", currentAction.Type)
	}
}

func (scriptExecutor *Executor) executeMouseMove(currentAction model.Action) error {
	if currentAction.Source == "found" {
		targetX := scriptExecutor.foundPositionX + currentAction.OffsetX
		targetY := scriptExecutor.foundPositionY + currentAction.OffsetY
		return scriptExecutor.inputSimulator.MoveMouse(targetX, targetY)
	}
	return scriptExecutor.inputSimulator.MoveMouse(currentAction.X, currentAction.Y)
}

func (scriptExecutor *Executor) executeDelay(ctx context.Context, currentAction model.Action) error {
	delayTimer := time.NewTimer(time.Duration(currentAction.DelayMs) * time.Millisecond)
	defer delayTimer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-delayTimer.C:
		return nil
	}
}

func (scriptExecutor *Executor) executeLoop(ctx context.Context, currentAction model.Action) error {
	if currentAction.LoopCount == 0 {
		// Infinite loop — runs until context cancelled
		for {
			if err := scriptExecutor.executeActions(ctx, currentAction.Actions); err != nil {
				return err
			}
			if err := nextIteration(ctx, len(currentAction.Actions) == 0); err != nil {
				return err
			}
		}
	}
	for iteration := 0; iteration < currentAction.LoopCount; iteration++ {
		if err := scriptExecutor.executeActions(ctx, currentAction.Actions); err != nil {
			return err
		}
	}
	return nil
}

func (scriptExecutor *Executor) executeWaitForImage(ctx context.Context, currentAction model.Action) error {
	if scriptExecutor.imageDetector == nil {
		return fmt.Errorf("image detector not available")
	}

	timeoutDuration := time.Duration(currentAction.TimeoutMs) * time.Millisecond
	deadlineTimer := time.NewTimer(timeoutDuration)
	defer deadlineTimer.Stop()

	for {
		foundX, foundY, imageFound, err := scriptExecutor.imageDetector.FindImage(currentAction.Image, currentAction.SearchRegion)
		if err != nil {
			return fmt.Errorf("image detection error: %w", err)
		}
		if imageFound {
			if currentAction.UseFoundPosition {
				scriptExecutor.foundPositionX = foundX
				scriptExecutor.foundPositionY = foundY
			}
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadlineTimer.C:
			return fmt.Errorf("wait_for_image timed out after %dms", currentAction.TimeoutMs)
		case <-time.After(waitPollInterval):
			// continue polling
		}
	}
}

func (scriptExecutor *Executor) executeIfImage(ctx context.Context, currentAction model.Action) error {
	if scriptExecutor.imageDetector == nil {
		return fmt.Errorf("image detector not available")
	}

	foundX, foundY, imageFound, err := scriptExecutor.imageDetector.FindImage(currentAction.Image, currentAction.SearchRegion)
	if err != nil {
		return fmt.Errorf("image detection error: %w", err)
	}

	if imageFound {
		if currentAction.UseFoundPosition {
			scriptExecutor.foundPositionX = foundX
			scriptExecutor.foundPositionY = foundY
		}
		return scriptExecutor.executeActions(ctx, currentAction.ThenActions)
	}

	if len(currentAction.ElseActions) > 0 {
		return scriptExecutor.executeActions(ctx, currentAction.ElseActions)
	}
	return nil
}

// sleepCtx waits for d or until ctx is cancelled; tests replace it to observe delays.
var sleepCtx = func(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// executeKeyPress types the action's key sequence in order, pausing KeyDelay
// between keys (not after the last). Cancellation is checked before each key and
// interrupts every wait; whatever is held is released on any exit path.
func (scriptExecutor *Executor) executeKeyPress(ctx context.Context, currentAction model.Action) error {
	sequence := currentAction.Sequence()
	if len(sequence) == 0 {
		return fmt.Errorf("key_press has no keys")
	}
	delay := time.Duration(currentAction.KeyDelay()) * time.Millisecond
	for i, stroke := range sequence {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := scriptExecutor.pressStroke(ctx, stroke); err != nil {
			return err
		}
		if i < len(sequence)-1 && delay > 0 {
			if err := sleepCtx(ctx, delay); err != nil {
				return err
			}
		}
	}
	return nil
}

func (scriptExecutor *Executor) pressStroke(ctx context.Context, stroke model.KeyStroke) error {
	var held []string
	defer func() {
		for j := len(held) - 1; j >= 0; j-- {
			_ = scriptExecutor.inputSimulator.KeyUp(held[j])
		}
	}()
	for _, name := range append(append([]string(nil), stroke.Mods...), stroke.Key) {
		if err := scriptExecutor.inputSimulator.KeyDown(name); err != nil {
			return err
		}
		held = append(held, name)
	}
	return sleepCtx(ctx, time.Duration(scriptExecutor.keyPressDurationMs)*time.Millisecond)
}

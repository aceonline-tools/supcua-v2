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
	MouseLeftDown()
	MouseLeftUp()
	MouseRightDown()
	MouseRightUp()
	KeyDown(keyName string) error
	KeyUp(keyName string) error
}

// ImageDetectorInterface abstracts image detection for testing.
type ImageDetectorInterface interface {
	FindImage(imageBase64 string, searchRegion *model.Region) (foundX int, foundY int, found bool, err error)
}

// StatusCallback is called when the executor status changes.
type StatusCallback func(scriptID string, status string, errorMessage string)

// Executor runs script actions line by line.
type Executor struct {
	inputSimulator InputSimulatorInterface
	imageDetector  ImageDetectorInterface
	foundPositionX int
	foundPositionY int
}

// NewExecutor creates an Executor with the given input simulator and image detector.
func NewExecutor(inputSimulator InputSimulatorInterface, imageDetector ImageDetectorInterface) *Executor {
	return &Executor{
		inputSimulator: inputSimulator,
		imageDetector:  imageDetector,
	}
}

// Execute runs a script's actions. Blocks until complete, cancelled, or error.
func (scriptExecutor *Executor) Execute(ctx context.Context, script model.Script) error {
	for {
		if err := scriptExecutor.executeActions(ctx, script.Actions); err != nil {
			return err
		}
		if script.RunMode != model.RunModeInfinite {
			return nil
		}
	}
}

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
			loopBody := actionList[loopStart+1 : j-1]
			count := currentAction.LoopCount
			if count == 0 {
				for {
					if err := scriptExecutor.executeActions(ctx, loopBody); err != nil {
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

		log.Printf("executing action: type=%s key=%s x=%d y=%d", currentAction.Type, currentAction.Key, currentAction.X, currentAction.Y)
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
		if err := scriptExecutor.inputSimulator.KeyDown(currentAction.Key); err != nil {
			return err
		}
		return scriptExecutor.inputSimulator.KeyUp(currentAction.Key)

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

	pollInterval := 200 * time.Millisecond
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
		case <-time.After(pollInterval):
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

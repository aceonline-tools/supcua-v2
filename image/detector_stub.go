//go:build !windows

package image

import (
	"fmt"

	"github.com/yanmad27/supcua-v2/model"
)

type Detector struct {
	matchTolerance int
}

func NewDetector(matchTolerance int) *Detector {
	return &Detector{matchTolerance: matchTolerance}
}

func (imageDetector *Detector) FindImage(imageBase64 string, searchRegion *model.Region) (int, int, bool, error) {
	return 0, 0, false, fmt.Errorf("image detection only supported on Windows")
}

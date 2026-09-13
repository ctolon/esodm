package guide

import (
	"context"
	"fmt"
	"strings"

	"github.com/ctolon/esodm"
)

// ProductHooks demonstrates normalization and validation without external side effects.
// Optional callbacks remain nil. Full-document validation does not run for patches.
func ProductHooks() esodm.Hooks[Product] {
	return esodm.Hooks[Product]{
		BeforeWrite: func(_ context.Context, _ esodm.Operation, product *Product) error {
			product.Name = strings.TrimSpace(product.Name)
			return nil
		},
		Validate: func(_ context.Context, product *Product) error {
			if product.Name == "" || product.Price < 0 {
				return fmt.Errorf("name is required and price must be nonnegative")
			}
			return nil
		},
		BeforePatch: func(_ context.Context, _ string, patch esodm.Patch) error {
			if _, changesName := patch["name"]; changesName {
				name, ok := patch["name"].(string)
				if !ok || strings.TrimSpace(name) == "" {
					return fmt.Errorf("%w: name must be a nonempty string", esodm.ErrValidation)
				}
				patch["name"] = strings.TrimSpace(name)
			}
			return nil
		},
	}
}

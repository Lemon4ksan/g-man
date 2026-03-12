// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package tf2

import (
	"errors"
	"time"

	"github.com/lemon4ksan/g-man/pkg/log"
)

func (t *TF2) jobWorker() {
	t.logger.Info("TF2 GC Job worker started")

	for {
		select {
		case <-t.ctx.Done():
			t.logger.Info("TF2 GC Job worker stopped")
			return
		case job := <-t.jobQueue:
			t.waitForGCConnection()

			job.Result <- t.processJob(job)

			time.Sleep(300 * time.Millisecond)
		}
	}
}

func (t *TF2) waitForGCConnection() {
	for t.state.Load() != int32(GCConnected) {
		select {
		case <-time.After(1 * time.Second):
		case <-t.ctx.Done():
			return
		}
	}
}

func (t *TF2) processJob(job *GCJob) error {
	t.logger.Debug("Processing GC Job", log.String("type", string(job.Type)))

	switch job.Type {
	case JobTypeSmelt, JobTypeCombine:
		return t.handleCraftMetalJob(job)
	case JobTypeCombineWeapon:
		return t.handleCraftWeaponJob(job)
	case JobTypeUse, JobTypeDelete:
		return t.handleUseOrDeleteJob(job)
	default:
		return errors.New("unknown job type")
	}
}

func (t *TF2) handleCraftMetalJob(job *GCJob) error {
	items := t.backpack.GetItemsByDefindex(job.DefIndex)

	required := 3
	if job.Type == JobTypeSmelt {
		required = 1
	}

	if len(items) < required {
		return errors.New("not enough items in backpack to craft")
	}

	targetIDs := items[:required]

	_, err := t.Craft(t.ctx, targetIDs, -1)
	return err
}

func (t *TF2) handleCraftWeaponJob(job *GCJob) error {
	if len(job.AssetIDs) != 2 {
		return errors.New("need exactly 2 asset IDs to craft weapons")
	}

	for _, id := range job.AssetIDs {
		if !t.backpack.HasItem(id) {
			return errors.New("item not in backpack")
		}
	}

	_, err := t.Craft(t.ctx, job.AssetIDs, -1)
	return err
}

func (t *TF2) handleUseOrDeleteJob(job *GCJob) error {
	if len(job.AssetIDs) < 1 {
		return errors.New("missing asset ID")
	}

	assetID := job.AssetIDs[0]
	if !t.backpack.HasItem(assetID) {
		return errors.New("item not found in backpack")
	}

	var err error
	if job.Type == JobTypeUse {
		err = t.UseItem(t.ctx, assetID)
	} else {
		err = t.DeleteItem(t.ctx, assetID)
	}

	if err != nil {
		return err
	}

	sub := t.bus.Subscribe(&ItemRemovedEvent{})
	defer sub.Unsubscribe()

	globalTimeout := time.After(10 * time.Second)

	for {
		select {
		case ev := <-sub.C():
			removedEvent := ev.(*ItemRemovedEvent)
			if removedEvent.ItemID != assetID {
				continue
			}

			debounce := time.After(1 * time.Second)

			for {
				select {
				case <-sub.C():
					debounce = time.After(1 * time.Second)
				case <-debounce:
					return nil
				case <-globalTimeout:
					return errors.New("global timeout reached during post-use debounce")
				}
			}

		case <-globalTimeout:
			return errors.New("timed out waiting for item to be removed from backpack")
		}
	}
}

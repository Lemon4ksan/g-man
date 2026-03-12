// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package tf2

type JobType string

const (
	JobTypeSmelt         JobType = "smelt"
	JobTypeCombine       JobType = "combine"
	JobTypeCombineWeapon JobType = "combineWeapon"
	JobTypeUse           JobType = "use"
	JobTypeDelete        JobType = "delete"
	JobTypeSort          JobType = "sort"
)

// GCJob represents a task for the Game Coordinator.
type GCJob struct {
	Type     JobType
	DefIndex uint32
	AssetIDs []uint64
	Result   chan error
}

func newJob(jobType JobType) *GCJob {
	return &GCJob{
		Type:   jobType,
		Result: make(chan error, 1), // Buffered channel so the worker doesn't block
	}
}

// EnqueueSmeltMetal queues metal smelting (Defindex 5001 -> 3x 5000).
func (t *TF2) EnqueueSmeltMetal(defIndex uint32) <-chan error {
	job := newJob(JobTypeSmelt)
	job.DefIndex = defIndex
	t.jobQueue <- job
	return job.Result
}

// EnqueueCombineMetal queues metal forging (3x 5000 -> 5001).
func (t *TF2) EnqueueCombineMetal(defIndex uint32) <-chan error {
	job := newJob(JobTypeCombine)
	job.DefIndex = defIndex
	t.jobQueue <- job
	return job.Result
}

// EnqueueCombineWeapons queues forging of two weapons.
func (t *TF2) EnqueueCombineWeapons(assetID1, assetID2 uint64) <-chan error {
	job := newJob(JobTypeCombineWeapon)
	job.AssetIDs = []uint64{assetID1, assetID2}
	t.jobQueue <- job
	return job.Result
}

// EnqueueUseItem queues the item for use (pending removal).
func (t *TF2) EnqueueUseItem(assetID uint64) <-chan error {
	job := newJob(JobTypeUse)
	job.AssetIDs = []uint64{assetID}
	t.jobQueue <- job
	return job.Result
}

// EnqueueDeleteItem queues an item for deletion.
func (t *TF2) EnqueueDeleteItem(assetID uint64) <-chan error {
	job := newJob(JobTypeDelete)
	job.AssetIDs = []uint64{assetID}
	t.jobQueue <- job
	return job.Result
}

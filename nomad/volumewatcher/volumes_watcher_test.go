package volumewatcher

import (
	"context"
	"testing"
	"time"

	memdb "github.com/hashicorp/go-memdb"
	"github.com/hashicorp/nomad/helper/testlog"
	"github.com/hashicorp/nomad/nomad/mock"
	"github.com/hashicorp/nomad/nomad/state"
	"github.com/hashicorp/nomad/nomad/structs"
	"github.com/stretchr/testify/require"
)

// TestVolumeWatch_EnableDisable tests the watcher registration logic that needs
// to happen during leader step-up/step-down
func TestVolumeWatch_EnableDisable(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	srv := &MockRPCServer{}
	srv.state = state.TestStateStore(t)
	index := uint64(100)

	watcher := NewVolumesWatcher(testlog.HCLogger(t),
		srv, srv,
		LimitStateQueriesPerSecond,
		CrossVolumeUpdateBatchDuration)

	watcher.SetEnabled(true, srv.State())

	plugin := mock.CSIPlugin()
	node := testNode(nil, plugin, srv.State())
	alloc := mock.Alloc()
	alloc.ClientStatus = structs.AllocClientStatusComplete
	vol := testVolume(nil, plugin, alloc, node.ID)

	index++
	err := srv.State().CSIVolumeRegister(index, []*structs.CSIVolume{vol})
	require.NoError(err)

	claim := &structs.CSIVolumeClaimRequest{VolumeID: vol.ID}
	claim.Namespace = vol.Namespace

	_, err = watcher.Reap(claim)
	require.NoError(err)
	require.Equal(1, len(watcher.watchers))

	watcher.SetEnabled(false, srv.State())
	require.Equal(0, len(watcher.watchers))
}

// TestVolumeWatch_Checkpoint tests the checkpointing of progress across
// leader leader step-up/step-down
func TestVolumeWatch_Checkpoint(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	srv := &MockRPCServer{}
	srv.state = state.TestStateStore(t)
	index := uint64(100)

	watcher := NewVolumesWatcher(testlog.HCLogger(t),
		srv, srv,
		LimitStateQueriesPerSecond,
		CrossVolumeUpdateBatchDuration)

	plugin := mock.CSIPlugin()
	node := testNode(nil, plugin, srv.State())
	alloc := mock.Alloc()
	alloc.ClientStatus = structs.AllocClientStatusComplete
	vol := testVolume(nil, plugin, alloc, node.ID)

	watcher.SetEnabled(true, srv.State())

	index++
	err := srv.State().CSIVolumeRegister(index, []*structs.CSIVolume{vol})
	require.NoError(err)

	// we should get or start up a watcher when we get an update for
	// the volume from the state store
	require.Eventually(func() bool {
		return 1 == len(watcher.watchers)
	}, time.Second, 10*time.Millisecond)

	// step-down (this is sync, but step-up is async)
	watcher.SetEnabled(false, srv.State())
	require.Equal(0, len(watcher.watchers))

	// step-up again
	watcher.SetEnabled(true, srv.State())
	require.Eventually(func() bool {
		return 1 == len(watcher.watchers)
	}, time.Second, 10*time.Millisecond)
}

// TestVolumeWatch_DetectVolume tests the detection of new volumes
func TestVolumeWatch_DetectVolume(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	srv := &MockBatchingRPCServer{}
	srv.state = state.TestStateStore(t)
	index := uint64(100)

	watcher := NewVolumesWatcher(testlog.HCLogger(t),
		srv, srv,
		LimitStateQueriesPerSecond,
		CrossVolumeUpdateBatchDuration)

	watcher.SetEnabled(true, srv.State())
	require.Equal(0, len(watcher.watchers))

	plugin := mock.CSIPlugin()
	node := testNode(nil, plugin, srv.State())
	alloc := mock.Alloc()
	alloc.ClientStatus = structs.AllocClientStatusComplete
	vol := testVolume(nil, plugin, alloc, node.ID)

	index++
	err := srv.State().CSIVolumeRegister(index, []*structs.CSIVolume{vol})
	require.NoError(err)

	require.Eventually(func() bool {
		return 1 == len(watcher.watchers)
	}, time.Second, 10*time.Millisecond)
}

// TestVolumeWatch_EndtoEndTrigger tests the triggering mechanism for
// updates end-to-end
func TestVolumeWatch_EndtoEndTrigger(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	ctx, exitFn := context.WithCancel(context.Background())
	defer exitFn()

	srv := &MockStatefulRPCServer{}
	srv.state = state.TestStateStore(t)
	srv.volumeUpdateBatcher = NewVolumeUpdateBatcher(CrossVolumeUpdateBatchDuration, srv, ctx)

	index := uint64(100)

	watcher := NewVolumesWatcher(testlog.HCLogger(t),
		srv, srv,
		LimitStateQueriesPerSecond,
		CrossVolumeUpdateBatchDuration)

	watcher.SetEnabled(true, srv.State())
	require.Equal(0, len(watcher.watchers))

	plugin := mock.CSIPlugin()
	node := testNode(nil, plugin, srv.State())
	alloc := mock.Alloc()
	alloc.ClientStatus = structs.AllocClientStatusComplete
	vol := testVolume(nil, plugin, alloc, node.ID)

	index++
	err := srv.State().CSIVolumeRegister(index, []*structs.CSIVolume{vol})
	require.NoError(err)

	require.Eventually(func() bool {
		return 1 == len(watcher.watchers)
	}, time.Second, 10*time.Millisecond)

	w := watcher.watchers[vol.ID+vol.Namespace]
	w.Notify(vol)

	require.Eventually(func() bool {
		ws := memdb.NewWatchSet()
		vol, _ := srv.State().CSIVolumeByID(ws, vol.Namespace, vol.ID)
		return len(vol.ReadAllocs) == 0 && len(vol.PastClaims) == 0
	}, time.Second*2, 10*time.Millisecond)

	require.Equal(1, srv.countCSINodeDetachVolume, "node detach RPC count")
	require.Equal(1, srv.countCSIControllerDetachVolume, "controller detach RPC count")
	require.Equal(2, srv.countUpsertVolumeClaims, "upsert claims count")
}

/*
Copyright IBM Corp. 2016 All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package fileledger

import (
	"errors"
	"io/ioutil"
	"os"
	"testing"

	"github.com/hyperledger/fabric/common/ledger/blockledger"
	"github.com/hyperledger/fabric/common/ledger/blockledger/fileledger/mock"
	"github.com/hyperledger/fabric/common/metrics/disabled"
	"github.com/stretchr/testify/require"
)

func TestBlockStoreProviderErrors(t *testing.T) {
	mockBlockStore := &mock.BlockStoreProvider{}
	f := &fileLedgerFactory{
		blkstorageProvider: mockBlockStore,
		ledgers:            map[string]blockledger.ReadWriter{},
	}

	t.Run("list", func(t *testing.T) {
		mockBlockStore.ListReturns(nil, errors.New("boogie"))
		require.PanicsWithValue(
			t,
			"boogie",
			func() { f.ChannelIDs() },
			"Expected ChannelIDs to panic if storage provider cannot list channel IDs",
		)
	})

	t.Run("open", func(t *testing.T) {
		mockBlockStore.OpenReturns(nil, errors.New("woogie"))
		_, err := f.GetOrCreate("foo")
		require.EqualError(t, err, "woogie")
		require.Empty(t, f.ledgers, "Expected no new ledger is created")
	})

	t.Run("remove", func(t *testing.T) {
		mockBlockStore.DropReturns(errors.New("oogie"))
		err := f.Remove("foo")
		require.EqualError(t, err, "oogie")
	})
}

func TestMultiReinitialization(t *testing.T) {
	metricsProvider := &disabled.Provider{}

	dir, err := ioutil.TempDir("", "fileledger")
	require.NoError(t, err, "Error creating temp dir: %s", err)
	defer os.RemoveAll(dir)

	f, err := New(dir, metricsProvider)
	require.NoError(t, err)
	_, err = f.GetOrCreate("testchannelid")
	require.NoError(t, err, "Error GetOrCreate channel")
	require.Equal(t, 1, len(f.ChannelIDs()), "Expected 1 channel")
	f.Close()

	f, err = New(dir, metricsProvider)
	require.NoError(t, err)
	_, err = f.GetOrCreate("foo")
	require.NoError(t, err, "Error creating channel")
	require.Equal(t, 2, len(f.ChannelIDs()), "Expected channel to be recovered")
	f.Close()

	f, err = New(dir, metricsProvider)
	require.NoError(t, err)
	_, err = f.GetOrCreate("bar")
	require.NoError(t, err, "Error creating channel")
	require.Equal(t, 3, len(f.ChannelIDs()), "Expected channel to be recovered")
	f.Close()

	f, err = New(dir, metricsProvider)
	require.NoError(t, err)
	err = f.Remove("bar")
	require.NoError(t, err, "Error removing channel")
	require.Equal(t, 2, len(f.ChannelIDs()))
	err = f.Remove("this-isnt-an-existing-channel")
	require.NoError(t, err, "Error removing channel")
	require.Equal(t, 2, len(f.ChannelIDs()))
	f.Close()
}

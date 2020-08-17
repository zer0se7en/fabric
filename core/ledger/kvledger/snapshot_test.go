/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package kvledger

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/golang/protobuf/proto"
	"github.com/hyperledger/fabric-protos-go/common"
	"github.com/hyperledger/fabric-protos-go/peer"
	pb "github.com/hyperledger/fabric-protos-go/peer"
	"github.com/hyperledger/fabric/bccsp/sw"
	"github.com/hyperledger/fabric/common/ledger/testutil"
	"github.com/hyperledger/fabric/common/metrics/disabled"
	"github.com/hyperledger/fabric/common/util"
	"github.com/hyperledger/fabric/core/chaincode/implicitcollection"
	"github.com/hyperledger/fabric/core/ledger"
	lgr "github.com/hyperledger/fabric/core/ledger"
	"github.com/hyperledger/fabric/core/ledger/internal/version"
	"github.com/hyperledger/fabric/core/ledger/kvledger/msgs"
	"github.com/hyperledger/fabric/core/ledger/mock"
	"github.com/hyperledger/fabric/protoutil"
	"github.com/stretchr/testify/require"
)

func TestSnapshotGenerationAndNewLedgerCreation(t *testing.T) {
	conf, cleanup := testConfig(t)
	defer cleanup()
	snapshotRootDir := conf.SnapshotsConfig.RootDir
	nsCollBtlConfs := []*nsCollBtlConfig{
		{
			namespace: "ns",
			btlConfig: map[string]uint64{"coll": 0},
		},
	}
	provider := testutilNewProviderWithCollectionConfig(
		t,
		nsCollBtlConfs,
		conf,
	)
	defer provider.Close()

	// add the genesis block and generate the snapshot
	blkGenerator, genesisBlk := testutil.NewBlockGenerator(t, "testLedgerid", false)
	lgr, err := provider.CreateFromGenesisBlock(genesisBlk)
	require.NoError(t, err)
	defer lgr.Close()
	kvlgr := lgr.(*kvLedger)
	require.NoError(t, kvlgr.generateSnapshot())
	verifySnapshotOutput(t,
		&expectedSnapshotOutput{
			snapshotRootDir:   snapshotRootDir,
			ledgerID:          kvlgr.ledgerID,
			ledgerHeight:      1,
			lastBlockHash:     protoutil.BlockHeaderHash(genesisBlk.Header),
			previousBlockHash: genesisBlk.Header.PreviousHash,
			lastCommitHash:    kvlgr.commitHash,
			stateDBType:       simpleKeyValueDB,
			expectedBinaryFiles: []string{
				"txids.data", "txids.metadata",
			},
		},
	)

	// add block-1 only with public state data and generate the snapshot
	blockAndPvtdata1 := prepareNextBlockForTest(t, kvlgr, blkGenerator, "SimulateForBlk1",
		map[string]string{
			"key1": "value1.1",
			"key2": "value2.1",
			"key3": "value3.1",
		},
		nil,
	)
	require.NoError(t, kvlgr.CommitLegacy(blockAndPvtdata1, &ledger.CommitOptions{}))
	require.NoError(t, kvlgr.generateSnapshot())
	verifySnapshotOutput(t,
		&expectedSnapshotOutput{
			snapshotRootDir:   snapshotRootDir,
			ledgerID:          kvlgr.ledgerID,
			ledgerHeight:      2,
			lastBlockHash:     protoutil.BlockHeaderHash(blockAndPvtdata1.Block.Header),
			previousBlockHash: blockAndPvtdata1.Block.Header.PreviousHash,
			lastCommitHash:    kvlgr.commitHash,
			stateDBType:       simpleKeyValueDB,
			expectedBinaryFiles: []string{
				"txids.data", "txids.metadata",
				"public_state.data", "public_state.metadata",
			},
		},
	)

	// add block-2 only with public and private data and generate the snapshot
	blockAndPvtdata2 := prepareNextBlockForTest(t, kvlgr, blkGenerator, "SimulateForBlk2",
		map[string]string{
			"key1": "value1.2",
			"key2": "value2.2",
			"key3": "value3.2",
		},
		map[string]string{
			"key1": "pvtValue1.2",
			"key2": "pvtValue2.2",
			"key3": "pvtValue3.2",
		},
	)
	require.NoError(t, kvlgr.CommitLegacy(blockAndPvtdata2, &ledger.CommitOptions{}))
	require.NoError(t, kvlgr.generateSnapshot())
	verifySnapshotOutput(t,
		&expectedSnapshotOutput{
			snapshotRootDir:   snapshotRootDir,
			ledgerID:          kvlgr.ledgerID,
			ledgerHeight:      3,
			lastBlockHash:     protoutil.BlockHeaderHash(blockAndPvtdata2.Block.Header),
			previousBlockHash: blockAndPvtdata2.Block.Header.PreviousHash,
			lastCommitHash:    kvlgr.commitHash,
			stateDBType:       simpleKeyValueDB,
			expectedBinaryFiles: []string{
				"txids.data", "txids.metadata",
				"public_state.data", "public_state.metadata",
				"private_state_hashes.data", "private_state_hashes.metadata",
			},
		},
	)

	// add dummy entry in collection config history and commit block-3 and generate the snapshot
	collConfigPkg := &peer.CollectionConfigPackage{
		Config: []*peer.CollectionConfig{
			{
				Payload: &peer.CollectionConfig_StaticCollectionConfig{
					StaticCollectionConfig: &peer.StaticCollectionConfig{
						Name: "coll",
					},
				},
			},
		},
	}
	addDummyEntryInCollectionConfigHistory(t, provider, kvlgr.ledgerID, "ns", 2, collConfigPkg)
	blockAndPvtdata3 := prepareNextBlockForTest(t, kvlgr, blkGenerator, "SimulateForBlk3",
		map[string]string{
			"key1": "value1.3",
			"key2": "value2.3",
			"key3": "value3.3",
		},
		nil,
	)
	require.NoError(t, kvlgr.CommitLegacy(blockAndPvtdata3, &ledger.CommitOptions{}))
	require.NoError(t, kvlgr.generateSnapshot())
	verifySnapshotOutput(t,
		&expectedSnapshotOutput{
			snapshotRootDir:   snapshotRootDir,
			ledgerID:          kvlgr.ledgerID,
			ledgerHeight:      4,
			lastBlockHash:     protoutil.BlockHeaderHash(blockAndPvtdata3.Block.Header),
			previousBlockHash: blockAndPvtdata3.Block.Header.PreviousHash,
			lastCommitHash:    kvlgr.commitHash,
			stateDBType:       simpleKeyValueDB,
			expectedBinaryFiles: []string{
				"txids.data", "txids.metadata",
				"public_state.data", "public_state.metadata",
				"private_state_hashes.data", "private_state_hashes.metadata",
				"confighistory.data", "confighistory.metadata",
			},
		},
	)

	snapshotDir := SnapshotDirForLedgerHeight(snapshotRootDir, kvlgr.ledgerID, 4)

	t.Run("create-ledger-from-snapshot", func(t *testing.T) {
		createdLedger := testCreateLedgerFromSnapshot(t, snapshotDir)
		verifyCreatedLedger(t,
			provider,
			createdLedger,
			&expectedLegderState{
				ledgerHeight:      4,
				currentBlockHash:  protoutil.BlockHeaderHash(blockAndPvtdata3.Block.Header),
				previousBlockHash: blockAndPvtdata3.Block.Header.PreviousHash,
				lastCommitHash:    kvlgr.commitHash,
				namespace:         "ns",
				publicState: map[string]string{
					"key1": "value1.3",
					"key2": "value2.3",
					"key3": "value3.3",
				},
				collectionConfig: map[uint64]*peer.CollectionConfigPackage{
					2: collConfigPkg,
				},
			},
		)
	})

	t.Run("create-ledger-from-snapshot-error-paths", func(t *testing.T) {
		testCreateLedgerFromSnapshotErrorPaths(t, snapshotDir)
	})

}

func TestSnapshotDBTypeCouchDB(t *testing.T) {
	conf, cleanup := testConfig(t)
	defer cleanup()
	provider := testutilNewProvider(conf, t, &mock.DeployedChaincodeInfoProvider{})
	defer provider.Close()
	lgr, err := provider.open("testLedger", nil)
	require.NoError(t, err)
	kvlgr := lgr.(*kvLedger)

	// artificially set the db type
	kvlgr.config.StateDBConfig.StateDatabase = ledger.CouchDB
	require.NoError(t, kvlgr.generateSnapshot())
	verifySnapshotOutput(t,
		&expectedSnapshotOutput{
			snapshotRootDir: conf.SnapshotsConfig.RootDir,
			ledgerID:        kvlgr.ledgerID,
			stateDBType:     ledger.CouchDB,
		},
	)
}

func TestSnapshotDirPaths(t *testing.T) {
	require.Equal(t, "/peerFSPath/snapshotRootDir/underConstruction", InProgressSnapshotsPath("/peerFSPath/snapshotRootDir"))
	require.Equal(t, "/peerFSPath/snapshotRootDir/completed", CompletedSnapshotsPath("/peerFSPath/snapshotRootDir"))
	require.Equal(t, "/peerFSPath/snapshotRootDir/completed/myLedger", SnapshotsDirForLedger("/peerFSPath/snapshotRootDir", "myLedger"))
	require.Equal(t, "/peerFSPath/snapshotRootDir/completed/myLedger/2000", SnapshotDirForLedgerHeight("/peerFSPath/snapshotRootDir", "myLedger", 2000))
}

func TestSnapshotDirPathsCreation(t *testing.T) {
	conf, cleanup := testConfig(t)
	defer cleanup()
	provider := testutilNewProvider(conf, t, &mock.DeployedChaincodeInfoProvider{})
	defer func() {
		provider.Close()
	}()

	inProgressSnapshotsPath := InProgressSnapshotsPath(conf.SnapshotsConfig.RootDir)
	completedSnapshotsPath := CompletedSnapshotsPath(conf.SnapshotsConfig.RootDir)

	// verify that upon first time start, kvledgerProvider creates an empty temp dir and an empty final dir for the snapshots
	for _, dir := range [2]string{inProgressSnapshotsPath, completedSnapshotsPath} {
		f, err := ioutil.ReadDir(dir)
		require.NoError(t, err)
		require.Len(t, f, 0)
	}

	// add a file in each of the above folders
	for _, dir := range [2]string{inProgressSnapshotsPath, completedSnapshotsPath} {
		err := ioutil.WriteFile(filepath.Join(dir, "testFile"), []byte("some junk data"), 0644)
		require.NoError(t, err)
		f, err := ioutil.ReadDir(dir)
		require.NoError(t, err)
		require.Len(t, f, 1)
	}

	// verify that upon subsequent opening, kvledgerProvider removes any under-processing snapshots,
	// potentially from a previous crash, from the temp dir but it does not remove any files from the final dir
	provider.Close()
	provider = testutilNewProvider(conf, t, &mock.DeployedChaincodeInfoProvider{})
	f, err := ioutil.ReadDir(inProgressSnapshotsPath)
	require.NoError(t, err)
	require.Len(t, f, 0)
	f, err = ioutil.ReadDir(completedSnapshotsPath)
	require.NoError(t, err)
	require.Len(t, f, 1)
}

func TestSnapshotsDirInitializingErrors(t *testing.T) {
	initKVLedgerProvider := func(conf *ledger.Config) error {
		cryptoProvider, err := sw.NewDefaultSecurityLevelWithKeystore(sw.NewDummyKeyStore())
		require.NoError(t, err)
		_, err = NewProvider(
			&lgr.Initializer{
				DeployedChaincodeInfoProvider: &mock.DeployedChaincodeInfoProvider{},
				MetricsProvider:               &disabled.Provider{},
				Config:                        conf,
				HashProvider:                  cryptoProvider,
			},
		)
		return err
	}

	t.Run("invalid-path", func(t *testing.T) {
		conf, cleanup := testConfig(t)
		defer cleanup()
		conf.SnapshotsConfig.RootDir = "./a-relative-path"
		err := initKVLedgerProvider(conf)
		require.EqualError(t, err, "invalid path: ./a-relative-path. The path for the snapshot dir is expected to be an absolute path")
	})

	t.Run("snapshots final dir creation returns error", func(t *testing.T) {
		conf, cleanup := testConfig(t)
		defer cleanup()

		completedSnapshotsPath := CompletedSnapshotsPath(conf.SnapshotsConfig.RootDir)
		require.NoError(t, os.MkdirAll(filepath.Dir(completedSnapshotsPath), 0755))
		require.NoError(t, ioutil.WriteFile(completedSnapshotsPath, []byte("some data"), 0644))
		err := initKVLedgerProvider(conf)
		require.Error(t, err)
		require.Contains(t, err.Error(), "while creating the dir: "+completedSnapshotsPath)
	})
}

func TestGenerateSnapshotErrors(t *testing.T) {
	conf, cleanup := testConfig(t)
	defer cleanup()
	provider := testutilNewProvider(conf, t, &mock.DeployedChaincodeInfoProvider{})
	defer func() {
		provider.Close()
	}()

	// create a ledger
	_, genesisBlk := testutil.NewBlockGenerator(t, "testLedgerid", false)
	lgr, err := provider.CreateFromGenesisBlock(genesisBlk)
	require.NoError(t, err)
	kvlgr := lgr.(*kvLedger)

	closeAndReopenLedgerProvider := func() {
		provider.Close()
		provider = testutilNewProvider(conf, t, &mock.DeployedChaincodeInfoProvider{})
		lgr, err = provider.Open("testLedgerid")
		require.NoError(t, err)
		kvlgr = lgr.(*kvLedger)
	}

	t.Run("snapshot tmp dir creation returns error", func(t *testing.T) {
		closeAndReopenLedgerProvider()
		require.NoError(t, os.RemoveAll( // remove the base tempdir so that the snapshot tempdir creation fails
			InProgressSnapshotsPath(conf.SnapshotsConfig.RootDir),
		))
		err := kvlgr.generateSnapshot()
		require.Error(t, err)
		require.Contains(t, err.Error(), "error while creating temp dir")
	})

	t.Run("block store returns error", func(t *testing.T) {
		closeAndReopenLedgerProvider()
		provider.blkStoreProvider.Close() // close the blockstore provider to trigger the error
		err := kvlgr.generateSnapshot()
		require.Error(t, err)
		errStackTrace := fmt.Sprintf("%+v", err)
		require.Contains(t, errStackTrace, "internal leveldb error while obtaining db iterator")
		require.Contains(t, errStackTrace, "fabric/common/ledger/blkstorage/blockindex.go")
	})

	t.Run("config history mgr returns error", func(t *testing.T) {
		closeAndReopenLedgerProvider()
		provider.configHistoryMgr.Close() // close the configHistoryMgr to trigger the error
		err := kvlgr.generateSnapshot()
		require.Error(t, err)
		errStackTrace := fmt.Sprintf("%+v", err)
		require.Contains(t, errStackTrace, "internal leveldb error while obtaining db iterator")
		require.Contains(t, errStackTrace, "fabric/core/ledger/confighistory/mgr.go")
	})

	t.Run("statedb returns error", func(t *testing.T) {
		closeAndReopenLedgerProvider()
		provider.dbProvider.Close() // close the dbProvider to trigger the error
		err := kvlgr.generateSnapshot()
		require.Error(t, err)
		errStackTrace := fmt.Sprintf("%+v", err)
		require.Contains(t, errStackTrace, "internal leveldb error while obtaining db iterator")
		require.Contains(t, errStackTrace, "statedb/stateleveldb/stateleveldb.go")
	})

	t.Run("renaming to the final snapshot dir returns error", func(t *testing.T) {
		closeAndReopenLedgerProvider()
		snapshotFinalDir := SnapshotDirForLedgerHeight(conf.SnapshotsConfig.RootDir, "testLedgerid", 1)
		require.NoError(t, os.MkdirAll(snapshotFinalDir, 0744))
		defer os.RemoveAll(snapshotFinalDir)
		require.NoError(t, ioutil.WriteFile( // make a non-empty snapshotFinalDir to trigger failure on rename
			filepath.Join(snapshotFinalDir, "dummyFile"),
			[]byte("dummy file"), 0444),
		)
		err := kvlgr.generateSnapshot()
		require.Contains(t, err.Error(), "error while renaming dir")
	})
}

func testCreateLedgerFromSnapshotErrorPaths(t *testing.T, originalSnapshotDir string) {
	var provider *Provider
	var snapshotDirForTest string
	var cleanup func()

	var metadata *snapshotMetadata
	var signableMetadataFile string
	var additionalMetadataFile string

	init := func(t *testing.T) {
		conf, cleanupFunc := testConfig(t)
		// make a copy of originalSnapshotDir
		snapshotDirForTest = filepath.Join(conf.RootFSPath, "snapshot")
		require.NoError(t, os.MkdirAll(snapshotDirForTest, 0700))
		files, err := ioutil.ReadDir(originalSnapshotDir)
		require.NoError(t, err)
		for _, f := range files {
			content, err := ioutil.ReadFile(filepath.Join(originalSnapshotDir, f.Name()))
			require.NoError(t, err)
			err = ioutil.WriteFile(filepath.Join(snapshotDirForTest, f.Name()), content, 0600)
			require.NoError(t, err)
		}

		metadataJSONs, err := loadSnapshotMetadataJSONs(snapshotDirForTest)
		require.NoError(t, err)
		metadata, err = metadataJSONs.toMetadata()
		require.NoError(t, err)

		signableMetadataFile = filepath.Join(snapshotDirForTest, snapshotSignableMetadataFileName)
		additionalMetadataFile = filepath.Join(snapshotDirForTest, snapshotAdditionalMetadataFileName)

		provider = testutilNewProvider(conf, t, &mock.DeployedChaincodeInfoProvider{})
		cleanup = func() {
			provider.Close()
			cleanupFunc()
		}
	}

	overwriteModifiedSignableMetadata := func() {
		signaleMetadataJSON, err := metadata.snapshotSignableMetadata.toJSON()
		require.NoError(t, err)
		require.NoError(t, ioutil.WriteFile(signableMetadataFile, signaleMetadataJSON, 0600))

		metadata.snapshotAdditionalMetadata.SnapshotHashInHex = computeHashForTest(t, provider, signaleMetadataJSON)
		additionalMetadataJSON, err := metadata.snapshotAdditionalMetadata.toJSON()
		require.NoError(t, err)
		require.NoError(t, ioutil.WriteFile(additionalMetadataFile, additionalMetadataJSON, 0600))
	}

	overwriteDataFile := func(fileName string, content []byte) {
		filePath := filepath.Join(snapshotDirForTest, fileName)
		require.NoError(t, ioutil.WriteFile(filePath, content, 0600))
		metadata.snapshotSignableMetadata.FilesAndHashes[fileName] = computeHashForTest(t, provider, content)
		overwriteModifiedSignableMetadata()
	}

	t.Run("singable-metadata-file-missing", func(t *testing.T) {
		init(t)
		defer cleanup()

		require.NoError(t, os.Remove(filepath.Join(snapshotDirForTest, snapshotSignableMetadataFileName)))
		_, err := provider.CreateFromSnapshot(snapshotDirForTest)
		require.EqualError(t,
			err,
			fmt.Sprintf(
				"error while loading metadata: open %s/_snapshot_signable_metadata.json: no such file or directory",
				snapshotDirForTest,
			),
		)
		verifyLedgerDoesNotExist(t, provider, metadata.ChannelName)
	})

	t.Run("additional-metadata-file-missing", func(t *testing.T) {
		init(t)
		defer cleanup()

		require.NoError(t, os.Remove(filepath.Join(snapshotDirForTest, snapshotAdditionalMetadataFileName)))
		_, err := provider.CreateFromSnapshot(snapshotDirForTest)
		require.EqualError(t,
			err,
			fmt.Sprintf("error while loading metadata: open %s/_snapshot_additional_metadata.json: no such file or directory", snapshotDirForTest),
		)
		verifyLedgerDoesNotExist(t, provider, metadata.ChannelName)
	})

	t.Run("singable-metadata-file-invalid-json", func(t *testing.T) {
		init(t)
		defer cleanup()

		require.NoError(t, ioutil.WriteFile(signableMetadataFile, []byte(""), 0600))
		_, err := provider.CreateFromSnapshot(snapshotDirForTest)
		require.EqualError(t,
			err,
			"error while unmarshaling metadata: error while unmarshaling signable metadata: unexpected end of JSON input",
		)
		verifyLedgerDoesNotExist(t, provider, metadata.ChannelName)
	})

	t.Run("additional-metadata-file-invalid-json", func(t *testing.T) {
		init(t)
		defer cleanup()

		require.NoError(t, ioutil.WriteFile(additionalMetadataFile, []byte(""), 0600))
		_, err := provider.CreateFromSnapshot(snapshotDirForTest)
		require.EqualError(t,
			err,
			"error while unmarshaling metadata: error while unmarshaling additional metadata: unexpected end of JSON input",
		)
		verifyLedgerDoesNotExist(t, provider, metadata.ChannelName)
	})

	t.Run("snapshot-hash-mismatch", func(t *testing.T) {
		init(t)
		defer cleanup()

		require.NoError(t, ioutil.WriteFile(signableMetadataFile, []byte("{}"), 0600))
		_, err := provider.CreateFromSnapshot(snapshotDirForTest)
		require.Contains(t,
			err.Error(),
			"error while verifying snapshot: hash mismatch for file [_snapshot_signable_metadata.json]",
		)
		verifyLedgerDoesNotExist(t, provider, metadata.ChannelName)
	})

	t.Run("datafile-missing", func(t *testing.T) {
		init(t)
		defer cleanup()

		err := os.Remove(filepath.Join(snapshotDirForTest, "txids.data"))
		require.NoError(t, err)

		_, err = provider.CreateFromSnapshot(snapshotDirForTest)
		require.EqualError(t, err,
			fmt.Sprintf(
				"error while verifying snapshot: open %s/txids.data: no such file or directory",
				snapshotDirForTest,
			),
		)
		verifyLedgerDoesNotExist(t, provider, metadata.ChannelName)
	})

	t.Run("datafile-hash-mismatch", func(t *testing.T) {
		init(t)
		defer cleanup()

		err := ioutil.WriteFile(filepath.Join(snapshotDirForTest, "txids.data"), []byte("random content"), 0600)
		require.NoError(t, err)

		_, err = provider.CreateFromSnapshot(snapshotDirForTest)
		require.Contains(t, err.Error(), "error while verifying snapshot: hash mismatch for file [txids.data]")
		verifyLedgerDoesNotExist(t, provider, metadata.ChannelName)
	})

	t.Run("hex-decoding-error-for-lastBlkHash", func(t *testing.T) {
		init(t)
		defer cleanup()

		metadata.snapshotSignableMetadata.LastBlockHashInHex = "invalid-hex"
		overwriteModifiedSignableMetadata()

		_, err := provider.CreateFromSnapshot(snapshotDirForTest)
		require.Contains(t, err.Error(), "error while decoding last block hash")
		verifyLedgerDoesNotExist(t, provider, metadata.ChannelName)
	})

	t.Run("hex-decoding-error-for-previousBlkHash", func(t *testing.T) {
		init(t)
		defer cleanup()

		metadata.snapshotSignableMetadata.PreviousBlockHashInHex = "invalid-hex"
		overwriteModifiedSignableMetadata()

		_, err := provider.CreateFromSnapshot(snapshotDirForTest)
		require.Contains(t, err.Error(), "error while decoding previous block hash")
		verifyLedgerDoesNotExist(t, provider, metadata.ChannelName)
	})

	t.Run("idStore-returns-error", func(t *testing.T) {
		init(t)
		defer cleanup()

		provider.idStore.close()
		_, err := provider.CreateFromSnapshot(snapshotDirForTest)
		require.Contains(t, err.Error(), "error while creating ledger id")
	})

	t.Run("blkstore-provider-returns-error", func(t *testing.T) {
		init(t)
		defer cleanup()

		overwriteDataFile("txids.data", []byte(""))
		_, err := provider.CreateFromSnapshot(snapshotDirForTest)
		require.Contains(t, err.Error(), "error while importing data into block store")
		verifyLedgerDoesNotExist(t, provider, metadata.ChannelName)
	})

	t.Run("config-history-mgr-returns-error", func(t *testing.T) {
		init(t)
		defer cleanup()

		overwriteDataFile("confighistory.data", []byte(""))
		_, err := provider.CreateFromSnapshot(snapshotDirForTest)
		require.Contains(t, err.Error(), "error while importing data into config history Mgr")
		verifyLedgerDoesNotExist(t, provider, metadata.ChannelName)
	})

	t.Run("statedb-provider-returns-error", func(t *testing.T) {
		init(t)
		defer cleanup()

		overwriteDataFile("public_state.data", []byte(""))
		_, err := provider.CreateFromSnapshot(snapshotDirForTest)
		require.Contains(t, err.Error(), "error while importing data into state db")
		verifyLedgerDoesNotExist(t, provider, metadata.ChannelName)
	})

	t.Run("error-while-deleting-partially-created-ledger", func(t *testing.T) {
		init(t)
		defer cleanup()

		provider.historydbProvider.Close()

		_, err := provider.CreateFromSnapshot(snapshotDirForTest)
		require.Contains(t, err.Error(), "error while preparing history db")
		require.Contains(t, err.Error(), "error while deleting data from ledger")
		verifyLedgerIDExists(t, provider, metadata.ChannelName, msgs.Status_UNDER_CONSTRUCTION)
	})
}

func computeHashForTest(t *testing.T, provider *Provider, content []byte) string {
	hasher, err := provider.initializer.HashProvider.GetHash(snapshotHashOpts)
	require.NoError(t, err)
	_, err = hasher.Write(content)
	require.NoError(t, err)
	return hex.EncodeToString(hasher.Sum(nil))
}

type expectedSnapshotOutput struct {
	snapshotRootDir     string
	ledgerID            string
	ledgerHeight        uint64
	lastBlockHash       []byte
	previousBlockHash   []byte
	lastCommitHash      []byte
	stateDBType         string
	expectedBinaryFiles []string
}

func verifySnapshotOutput(
	t *testing.T,
	o *expectedSnapshotOutput,
) {
	inProgressSnapshotsPath := InProgressSnapshotsPath(o.snapshotRootDir)
	f, err := ioutil.ReadDir(inProgressSnapshotsPath)
	require.NoError(t, err)
	require.Len(t, f, 0)

	snapshotDir := SnapshotDirForLedgerHeight(o.snapshotRootDir, o.ledgerID, o.ledgerHeight)
	files, err := ioutil.ReadDir(snapshotDir)
	require.NoError(t, err)
	require.Len(t, files, len(o.expectedBinaryFiles)+2) // + 2 JSON files

	filesAndHashes := map[string]string{}
	for _, f := range o.expectedBinaryFiles {
		c, err := ioutil.ReadFile(filepath.Join(snapshotDir, f))
		require.NoError(t, err)
		filesAndHashes[f] = hex.EncodeToString(util.ComputeSHA256(c))
	}

	// verify the contents of the file snapshot_metadata.json
	m := &snapshotSignableMetadata{}
	mJSON, err := ioutil.ReadFile(filepath.Join(snapshotDir, snapshotSignableMetadataFileName))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(mJSON, m))

	previousBlockHashHex := ""
	if o.previousBlockHash != nil {
		previousBlockHashHex = hex.EncodeToString(o.previousBlockHash)
	}
	require.Equal(t,
		&snapshotSignableMetadata{
			ChannelName:            o.ledgerID,
			ChannelHeight:          o.ledgerHeight,
			LastBlockHashInHex:     hex.EncodeToString(o.lastBlockHash),
			PreviousBlockHashInHex: previousBlockHashHex,
			StateDBType:            o.stateDBType,
			FilesAndHashes:         filesAndHashes,
		},
		m,
	)

	// verify the contents of the file snapshot_metadata_hash.json
	mh := &snapshotAdditionalMetadata{}
	mhJSON, err := ioutil.ReadFile(filepath.Join(snapshotDir, snapshotAdditionalMetadataFileName))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(mhJSON, mh))
	require.Equal(t,
		&snapshotAdditionalMetadata{
			SnapshotHashInHex:        hex.EncodeToString(util.ComputeSHA256(mJSON)),
			LastBlockCommitHashInHex: hex.EncodeToString(o.lastCommitHash),
		},
		mh,
	)
}

func testCreateLedgerFromSnapshot(t *testing.T, snapshotDir string) *kvLedger {
	conf, cleanup := testConfig(t)
	defer cleanup()
	p := testutilNewProvider(conf, t, &mock.DeployedChaincodeInfoProvider{})
	destLedger, err := p.CreateFromSnapshot(snapshotDir)
	require.NoError(t, err)
	return destLedger.(*kvLedger)
}

type expectedLegderState struct {
	ledgerHeight      uint64
	currentBlockHash  []byte
	previousBlockHash []byte
	lastCommitHash    []byte
	namespace         string
	publicState       map[string]string
	collectionConfig  map[uint64]*peer.CollectionConfigPackage
}

func verifyCreatedLedger(t *testing.T,
	p *Provider,
	l *kvLedger,
	e *expectedLegderState,
) {

	verifyLedgerIDExists(t, p, l.ledgerID, msgs.Status_ACTIVE)

	destBCInfo, err := l.GetBlockchainInfo()
	require.NoError(t, err)
	require.Equal(t,
		&common.BlockchainInfo{
			Height:            e.ledgerHeight,
			CurrentBlockHash:  e.currentBlockHash,
			PreviousBlockHash: e.previousBlockHash,
		},
		destBCInfo,
	)

	statedbSavepoint, err := l.txmgr.GetLastSavepoint()
	require.NoError(t, err)
	require.Equal(t, version.NewHeight(e.ledgerHeight-1, math.MaxUint64), statedbSavepoint)

	historydbSavepoint, err := l.historyDB.GetLastSavepoint()
	require.NoError(t, err)
	require.Equal(t, version.NewHeight(e.ledgerHeight-1, math.MaxUint64), historydbSavepoint)

	qe, err := l.txmgr.NewQueryExecutor("dummyTxId")
	require.NoError(t, err)
	defer qe.Done()
	for k, v := range e.publicState {
		val, err := qe.GetState(e.namespace, k)
		require.NoError(t, err)
		require.Equal(t, v, string(val))
	}
	for committingBlock, collConfigPkg := range e.collectionConfig {
		collConfigInfo, err := l.configHistoryRetriever.MostRecentCollectionConfigBelow(committingBlock+1, e.namespace)
		require.NoError(t, err)
		require.Equal(t, committingBlock, collConfigInfo.CommittingBlockNum)
		require.True(t, proto.Equal(collConfigPkg, collConfigInfo.CollectionConfig))
	}
}

func addDummyEntryInCollectionConfigHistory(
	t *testing.T,
	provider *Provider,
	ledgerID string,
	namespace string,
	committingBlockNumber uint64,
	collectionConfig *peer.CollectionConfigPackage,
) {
	// configure mock to cause data entry in collection config history
	ccInfoProviderMock := provider.initializer.DeployedChaincodeInfoProvider.(*mock.DeployedChaincodeInfoProvider)
	ccInfoProviderMock.UpdatedChaincodesReturns(
		[]*ledger.ChaincodeLifecycleInfo{
			{
				Name: "ns",
			},
		},
		nil,
	)

	ccInfoProviderMock.ChaincodeInfoReturns(
		&ledger.DeployedChaincodeInfo{
			Name:                        namespace,
			ExplicitCollectionConfigPkg: collectionConfig,
		},
		nil,
	)
	require.NoError(t,
		provider.configHistoryMgr.HandleStateUpdates(
			&ledger.StateUpdateTrigger{
				LedgerID:           ledgerID,
				CommittingBlockNum: committingBlockNumber,
				StateUpdates: map[string]*ledger.KVStateUpdates{
					namespace: {},
				},
			},
		),
	)
}

func TestMostRecentCollectionConfigFetcher(t *testing.T) {
	conf, cleanup := testConfig(t)
	defer cleanup()

	ledgerID := "test-ledger"
	chaincodeName := "test-chaincode"

	implicitCollectionName := implicitcollection.NameForOrg("test-org")
	implicitCollection := &pb.StaticCollectionConfig{
		Name: implicitCollectionName,
	}
	mockDeployedCCInfoProvider := &mock.DeployedChaincodeInfoProvider{}
	mockDeployedCCInfoProvider.GenerateImplicitCollectionForOrgReturns(implicitCollection)

	provider := testutilNewProvider(conf, t, mockDeployedCCInfoProvider)
	explicitCollectionName := "explicit-coll"
	explicitCollection := &pb.StaticCollectionConfig{
		Name: explicitCollectionName,
	}
	testutilPersistExplicitCollectionConfig(
		t,
		provider,
		mockDeployedCCInfoProvider,
		ledgerID,
		chaincodeName,
		testutilCollConfigPkg(
			[]*pb.StaticCollectionConfig{
				explicitCollection,
			},
		),
		10,
	)

	fetcher := &mostRecentCollectionConfigFetcher{
		DeployedChaincodeInfoProvider: mockDeployedCCInfoProvider,
		Retriever:                     provider.configHistoryMgr.GetRetriever(ledgerID),
	}

	testcases := []struct {
		name                 string
		lookupCollectionName string
		expectedOutput       *pb.StaticCollectionConfig
	}{
		{
			name:                 "lookup-implicit-collection",
			lookupCollectionName: implicitCollectionName,
			expectedOutput:       implicitCollection,
		},

		{
			name:                 "lookup-explicit-collection",
			lookupCollectionName: explicitCollectionName,
			expectedOutput:       explicitCollection,
		},

		{
			name:                 "lookup-non-existing-explicit-collection",
			lookupCollectionName: "non-existing-explicit-collection",
			expectedOutput:       nil,
		},
	}

	for _, testcase := range testcases {
		t.Run(
			testcase.name,
			func(t *testing.T) {
				config, err := fetcher.CollectionInfo(chaincodeName, testcase.lookupCollectionName)
				require.NoError(t, err)
				require.True(t, proto.Equal(testcase.expectedOutput, config))
			},
		)
	}

	t.Run("explicit-collection-lookup-causes-error", func(t *testing.T) {
		provider.configHistoryMgr.Close()
		_, err := fetcher.CollectionInfo(chaincodeName, explicitCollectionName)
		require.Contains(t, err.Error(), "error while fetching most recent collection config")
	})
}

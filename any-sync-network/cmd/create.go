package cmd

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/anyproto/any-sync/accountservice"
	"github.com/anyproto/any-sync/util/crypto"
	"github.com/spf13/cobra"
	"gopkg.in/mgo.v2/bson"
	"gopkg.in/yaml.v3"
)

type GeneralNodeConfig struct {
	Account accountservice.Config `yaml:"account"`
	Drpc    struct {
		Stream struct {
			MaxMsgSizeMb int `yaml:"maxMsgSizeMb"`
		} `yaml:"stream"`
	} `yaml:"drpc"`
	Yamux struct {
		ListenAddrs     []string `yaml:"listenAddrs"`
		WriteTimeoutSec int      `yaml:"writeTimeoutSec"`
		DialTimeoutSec  int      `yaml:"dialTimeoutSec"`
	} `yaml:"yamux"`
	Quic struct {
		ListenAddrs     []string `yaml:"listenAddrs"`
		WriteTimeoutSec int      `yaml:"writeTimeoutSec"`
		DialTimeoutSec  int      `yaml:"dialTimeoutSec"`
	} `yaml:"quic"`
	Network          Network `yaml:"network"`
	NetworkStorePath string  `yaml:"networkStorePath"`
}

type CoordinatorNodeConfig struct {
	GeneralNodeConfig `yaml:".,inline"`
	Mongo             struct {
		Connect  string `yaml:"connect"`
		Database string `yaml:"database"`
	} `yaml:"mongo"`
	SpaceStatus struct {
		RunSeconds         int `yaml:"runSeconds"`
		DeletionPeriodDays int `yaml:"deletionPeriodDays"`
	} `yaml:"spaceStatus"`
	DefaultLimits struct {
		SpaceMembersRead  int `yaml:"spaceMembersRead"`
		SpaceMembersWrite int `yaml:"spaceMembersWrite"`
		SharedSpacesLimit int `yaml:"sharedSpacesLimit"`
	} `yaml:"defaultLimits"`
}

type ConsensusNodeConfig struct {
	GeneralNodeConfig `yaml:".,inline"`
	Mongo             struct {
		Connect       string `yaml:"connect"`
		Database      string `yaml:"database"`
		LogCollection string `yaml:"logCollection"`
	} `yaml:"mongo"`
	NetworkUpdateIntervalSec int `yaml:"networkUpdateIntervalSec"`
}

type SyncNodeConfig struct {
	GeneralNodeConfig        `yaml:".,inline"`
	NetworkUpdateIntervalSec int `yaml:"networkUpdateIntervalSec"`
	Space                    struct {
		GcTTL      int `yaml:"gcTTL"`
		SyncPeriod int `yaml:"syncPeriod"`
	} `yaml:"space"`
	Storage struct {
		Path string `yaml:"path"`
	} `yaml:"storage"`
	NodeSync struct {
		SyncOnStart       bool `yaml:"syncOnStart"`
		PeriodicSyncHours int  `yaml:"periodicSyncHours"`
	} `yaml:"nodeSync"`
	Log struct {
		Production   bool   `yaml:"production"`
		DefaultLevel string `yaml:"defaultLevel"`
		NamedLevels  struct {
		} `yaml:"namedLevels"`
	} `yaml:"log"`
	ApiServer struct {
		ListenAddr string `yaml:"listenAddr"`
	} `yaml:"apiServer"`
}

type FileNodeConfig struct {
	GeneralNodeConfig        `yaml:".,inline"`
	NetworkUpdateIntervalSec int `yaml:"networkUpdateIntervalSec"`
	DefaultLimit             int `yaml:"defaultLimit"`
	S3Store                  struct {
		Endpoint   string `yaml:"endpoint,omitempty"`
		Region     string `yaml:"region"`
		Profile    string `yaml:"profile"`
		Bucket     string `yaml:"bucket"`
		MaxThreads int    `yaml:"maxThreads"`
	} `yaml:"s3Store"`
	Redis struct {
		IsCluster bool   `yaml:"isCluster"`
		URL       string `yaml:"url"`
	} `yaml:"redis"`
}

type Node struct {
	PeerID    string   `yaml:"peerId"`
	Addresses []string `yaml:"addresses"`
	Types     []string `yaml:"types"`
}

type HeartConfig struct {
	NetworkID string `yaml:"networkId"`
	Nodes     []Node `yaml:"nodes"`
}

type Network struct {
	ID           string `yaml:"id"`
	HeartConfig  `yaml:".,inline"`
	CreationTime time.Time `yaml:"creationTime"`
}

var create = &cobra.Command{
	Use:   "create",
	Short: "Creates new network configuration",
	Run: func(cmd *cobra.Command, args []string) {
		// Create Network
		fmt.Println("Creating network...")
		netKey, _, _ := crypto.GenerateRandomEd25519KeyPair()
		network = Network{
			HeartConfig: HeartConfig{
				Nodes: []Node{},
			},
		}
		network.ID = bson.NewObjectId().Hex()
		network.NetworkID = netKey.GetPublic().Network()
		network.CreationTime = time.Now()

		fmt.Println("\033[1m  Network ID:\033[0m", network.NetworkID)

		// Create coordinator node
		fmt.Println("\nCreating coordinator node...")

		coordinatorAs := struct {
			Address      string
			YamuxPort    string
			QuicPort     string
			MongoConnect string
			MongoDB      string
		}{
			Address:      "127.0.0.1",
			YamuxPort:    "4830",
			QuicPort:     "5830",
			MongoConnect: "mongodb://localhost:27017",
			MongoDB:      "coordinator",
		}

		coordinatorNode := defaultCoordinatorNode()
		coordinatorNode.Yamux.ListenAddrs = append(coordinatorNode.Yamux.ListenAddrs, coordinatorAs.Address+":"+coordinatorAs.YamuxPort)
		coordinatorNode.Quic.ListenAddrs = append(coordinatorNode.Quic.ListenAddrs, coordinatorAs.Address+":"+coordinatorAs.QuicPort)
		coordinatorNode.Mongo.Connect = coordinatorAs.MongoConnect
		coordinatorNode.Mongo.Database = coordinatorAs.MongoDB
		coordinatorNode.Account = generateAccount()
		coordinatorNode.Account.SigningKey, _ = crypto.EncodeKeyToString(netKey)

		addToNetwork(coordinatorNode.GeneralNodeConfig, "coordinator")

		// Create consensus node
		fmt.Println("\nCreating consensus node...")

		consensusAs := struct {
			Address   string
			YamuxPort string
			QuicPort  string
			MongoDB   string
		}{
			Address:   "127.0.0.1",
			YamuxPort: "4530",
			QuicPort:  "5530",
			MongoDB:   "consensus",
		}

		consensusNode := defaultConsensusNode()
		consensusNode.Yamux.ListenAddrs = append(consensusNode.Yamux.ListenAddrs, consensusAs.Address+":"+consensusAs.YamuxPort)
		consensusNode.Quic.ListenAddrs = append(consensusNode.Quic.ListenAddrs, consensusAs.Address+":"+consensusAs.QuicPort)
		consensusNode.Mongo.Database = consensusAs.MongoDB
		consensusNode.Account = generateAccount()

		addToNetwork(consensusNode.GeneralNodeConfig, "consensus")

		createSyncNode()

		createFileNode()

		lastStepOptions()

		// Create configurations for all nodes
		fmt.Println("\nCreating config file...")

		coordinatorNode.Network = network
		createConfigFile(coordinatorNode, "coordinator")

		consensusNode.Network = network
		createConfigFile(consensusNode, "consensus")

		for i, syncNode := range syncNodes {
			syncNode.Network = network
			createConfigFile(syncNode, "sync_"+strconv.Itoa(i+1))
		}

		for i, fileNode := range fileNodes {
			fileNode.Network = network
			createConfigFile(fileNode, "file_"+strconv.Itoa(i+1))
		}

		createConfigFile(network.HeartConfig, "heart")

		networkWrapper := map[string]interface{}{
			"network": network.HeartConfig,
		}
		createConfigFile(networkWrapper, "network")

		fmt.Println("Done!")
	},
}

var network = Network{}

func addToNetwork(node GeneralNodeConfig, nodeType string) {
	addresses := []string{}
	for _, addr := range node.Yamux.ListenAddrs {
		addresses = append(addresses, "yamux://"+addr)
	}
	for _, addr := range node.Quic.ListenAddrs {
		addresses = append(addresses, "quic://"+addr)
	}
	network.Nodes = append(network.Nodes, Node{
		PeerID:    node.Account.PeerId,
		Addresses: addresses,
		Types:     []string{nodeType},
	})
}

var syncNodeYamuxPort = "4430"
var syncNodeQuicPort = "5430"
var syncNodes = []SyncNodeConfig{}

func createSyncNode() {
	fmt.Println("\nCreating sync node...")

	answers := struct {
		Address   string
		YamuxPort string
		QuicPort  string
	}{
		Address:   "127.0.0.1",
		YamuxPort: syncNodeYamuxPort,
		QuicPort:  syncNodeQuicPort,
	}

	syncNode := defaultSyncNode()
	syncNode.Yamux.ListenAddrs = append(syncNode.Yamux.ListenAddrs, answers.Address+":"+answers.YamuxPort)
	syncNode.Quic.ListenAddrs = append(syncNode.Quic.ListenAddrs, answers.Address+":"+answers.QuicPort)
	syncNode.Account = generateAccount()

	addToNetwork(syncNode.GeneralNodeConfig, "tree")
	syncNodes = append(syncNodes, syncNode)

	// Increase sync node port
	port_num, _ := strconv.ParseInt(syncNodeYamuxPort, 10, 0)
	port_num += 1
	syncNodeYamuxPort = strconv.FormatInt(port_num, 10)
	port_num, _ = strconv.ParseInt(syncNodeQuicPort, 10, 0)
	port_num += 1
	syncNodeQuicPort = strconv.FormatInt(port_num, 10)
}

var fileNodeYamuxPort = "4730"
var fileNodeQuicPort = "5730"
var fileNodes = []FileNodeConfig{}

func createFileNode() {
	fmt.Println("\nCreating file node...")

	answers := struct {
		Address      string
		YamuxPort    string
		QuicPort     string
		S3Endpoint   string
		S3Region     string
		S3Profile    string
		S3Bucket     string
		RedisURL     string
		RedisCluster string
	}{
		Address:      "127.0.0.1",
		YamuxPort:    fileNodeYamuxPort,
		QuicPort:     fileNodeQuicPort,
		S3Endpoint:   "http://127.0.0.1:9000",
		S3Region:     "eu-central-1",
		S3Profile:    "default",
		S3Bucket:     "any-sync-files",
		RedisURL:     "redis://127.0.0.1:6379/?dial_timeout=3&read_timeout=6s",
		RedisCluster: "false",
	}

	fileNode := defaultFileNode()
	fileNode.Yamux.ListenAddrs = append(fileNode.Yamux.ListenAddrs, answers.Address+":"+answers.YamuxPort)
	fileNode.Quic.ListenAddrs = append(fileNode.Quic.ListenAddrs, answers.Address+":"+answers.QuicPort)
	fileNode.S3Store.Endpoint = answers.S3Endpoint
	fileNode.S3Store.Region = answers.S3Region
	fileNode.S3Store.Profile = answers.S3Profile
	fileNode.S3Store.Bucket = answers.S3Bucket
	fileNode.Redis.URL = answers.RedisURL
	fileNode.Redis.IsCluster, _ = strconv.ParseBool(answers.RedisCluster)
	fileNode.Account = generateAccount()

	addToNetwork(fileNode.GeneralNodeConfig, "file")
	fileNodes = append(fileNodes, fileNode)

	// Increase file node port
	port_num, _ := strconv.ParseInt(fileNodeYamuxPort, 10, 0)
	port_num += 1
	fileNodeYamuxPort = strconv.FormatInt(port_num, 10)
	port_num, _ = strconv.ParseInt(fileNodeQuicPort, 10, 0)
	port_num += 1
	fileNodeQuicPort = strconv.FormatInt(port_num, 10)
}

func lastStepOptions() {
	fmt.Println()
	// prompt := &survey.Select{
	// 	Default: "No, generate configs",
	// }

	option := ""
	// survey.AskOne(prompt, &option, survey.WithValidator(survey.Required))
	switch option {
	case "Add sync-node":
		createSyncNode()
		lastStepOptions()
	case "Add file-node":
		createFileNode()
		lastStepOptions()
	default:
		return
	}
}

func generateAccount() accountservice.Config {
	signKey, _, _ := crypto.GenerateRandomEd25519KeyPair()

	encPeerSignKey, err := crypto.EncodeKeyToString(signKey)
	if err != nil {
		return accountservice.Config{}
	}

	peerID := signKey.GetPublic().PeerId()

	return accountservice.Config{
		PeerId:     peerID,
		PeerKey:    encPeerSignKey,
		SigningKey: encPeerSignKey,
	}
}

func defaultGeneralNode() GeneralNodeConfig {
	return GeneralNodeConfig{
		Drpc: struct {
			Stream struct {
				MaxMsgSizeMb int "yaml:\"maxMsgSizeMb\""
			} "yaml:\"stream\""
		}{
			Stream: struct {
				MaxMsgSizeMb int "yaml:\"maxMsgSizeMb\""
			}{
				MaxMsgSizeMb: 256,
			},
		},
		Yamux: struct {
			ListenAddrs     []string "yaml:\"listenAddrs\""
			WriteTimeoutSec int      "yaml:\"writeTimeoutSec\""
			DialTimeoutSec  int      "yaml:\"dialTimeoutSec\""
		}{
			WriteTimeoutSec: 10,
			DialTimeoutSec:  10,
		},
		Quic: struct {
			ListenAddrs     []string "yaml:\"listenAddrs\""
			WriteTimeoutSec int      "yaml:\"writeTimeoutSec\""
			DialTimeoutSec  int      "yaml:\"dialTimeoutSec\""
		}{
			WriteTimeoutSec: 10,
			DialTimeoutSec:  10,
		},
		NetworkStorePath: "/networkStore",
	}
}

func defaultCoordinatorNode() CoordinatorNodeConfig {
	return CoordinatorNodeConfig{
		GeneralNodeConfig: defaultGeneralNode(),
		Mongo: struct {
			Connect  string "yaml:\"connect\""
			Database string "yaml:\"database\""
		}{},
		SpaceStatus: struct {
			RunSeconds         int "yaml:\"runSeconds\""
			DeletionPeriodDays int "yaml:\"deletionPeriodDays\""
		}{
			RunSeconds:         20,
			DeletionPeriodDays: 1,
		},
		DefaultLimits: struct {
			SpaceMembersRead  int "yaml:\"spaceMembersRead\""
			SpaceMembersWrite int "yaml:\"spaceMembersWrite\""
			SharedSpacesLimit int "yaml:\"sharedSpacesLimit\""
		}{
			SpaceMembersRead:  1000,
			SpaceMembersWrite: 1000,
			SharedSpacesLimit: 1000,
		},
	}
}

func defaultConsensusNode() ConsensusNodeConfig {
	return ConsensusNodeConfig{
		GeneralNodeConfig: defaultGeneralNode(),
		Mongo: struct {
			Connect       string "yaml:\"connect\""
			Database      string "yaml:\"database\""
			LogCollection string "yaml:\"logCollection\""
		}{
			LogCollection: "log",
		},
		NetworkUpdateIntervalSec: 600,
	}
}

func defaultSyncNode() SyncNodeConfig {
	return SyncNodeConfig{
		GeneralNodeConfig:        defaultGeneralNode(),
		NetworkUpdateIntervalSec: 600,
		Space: struct {
			GcTTL      int "yaml:\"gcTTL\""
			SyncPeriod int "yaml:\"syncPeriod\""
		}{
			GcTTL:      60,
			SyncPeriod: 240,
		},
		Storage: struct {
			Path string "yaml:\"path\""
		}{
			Path: "db",
		},
		NodeSync: struct {
			SyncOnStart       bool "yaml:\"syncOnStart\""
			PeriodicSyncHours int  "yaml:\"periodicSyncHours\""
		}{
			SyncOnStart:       true,
			PeriodicSyncHours: 2,
		},
		Log: struct {
			Production   bool     "yaml:\"production\""
			DefaultLevel string   "yaml:\"defaultLevel\""
			NamedLevels  struct{} "yaml:\"namedLevels\""
		}{
			Production:   false,
			DefaultLevel: "",
			NamedLevels:  struct{}{},
		},
		ApiServer: struct {
			ListenAddr string "yaml:\"listenAddr\""
		}{
			ListenAddr: "0.0.0.0:8080",
		},
	}
}

func defaultFileNode() FileNodeConfig {
	return FileNodeConfig{
		GeneralNodeConfig:        defaultGeneralNode(),
		NetworkUpdateIntervalSec: 600,
		DefaultLimit:             1073741824,
		S3Store: struct {
			Endpoint   string "yaml:\"endpoint,omitempty\""
			Region     string "yaml:\"region\""
			Profile    string "yaml:\"profile\""
			Bucket     string "yaml:\"bucket\""
			MaxThreads int    "yaml:\"maxThreads\""
		}{
			MaxThreads: 16,
		},
		Redis: struct {
			IsCluster bool   "yaml:\"isCluster\""
			URL       string "yaml:\"url\""
		}{},
	}
}

func createConfigFile(in interface{}, ymlFilename string) {
	bytes, err := yaml.Marshal(in)
	if err != nil {
		panic(fmt.Sprintf("Could not marshal the keys: %v", err))
	}

	err = os.WriteFile(ymlFilename+".yml", bytes, os.ModePerm)
	if err != nil {
		panic(fmt.Sprintf("Could not write the config to file: %v", err))
	}
}

func init() {
}

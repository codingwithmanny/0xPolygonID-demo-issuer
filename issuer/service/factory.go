package service

import (
	"github.com/iden3/go-iden3-crypto/babyjub"
	logger "github.com/sirupsen/logrus"
	database "issuer/db"
	"issuer/service/cfgs"
	"issuer/service/http"
	"issuer/service/identity"
	"issuer/service/identitystate"
	"os"
	"time"
)

func CreateApp(altCfgPath string) error {

	cfg, err := cfgs.New(altCfgPath)
	if err != nil {
		return err
	}

	err = initGlobalLogger(cfg.LogLevel)
	if err != nil {
		return err
	}

	db, err := database.New(cfg.DBFilePath)
	if err != nil {
		return err
	}

	// create identity state
	idenState, err := identitystate.New(db, cfg.MerkleTreeDepth)
	if err != nil {
		return err
	}

	issuer, err := identity.New(idenState, bytesToJubjubKey(cfg.SecretKey), cfg.IdentityHostUrl)
	if err != nil {
		return err
	}

	// start service
	s := http.NewServer(cfg.HttpListenAddress, issuer)
	return s.Run()
}

func bytesToJubjubKey(b []byte) babyjub.PrivateKey {
	var privKey babyjub.PrivateKey
	copy(privKey[:], b)

	return privKey
}

func initGlobalLogger(level string) error {
	logLevel, err := logger.ParseLevel(level)
	if err != nil {
		return err
	}

	logger.SetLevel(logLevel)
	logger.SetFormatter(&logger.JSONFormatter{TimestampFormat: time.RFC3339Nano})
	logger.SetOutput(os.Stdout)
	return nil
}

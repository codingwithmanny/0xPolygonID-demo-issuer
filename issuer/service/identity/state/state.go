package state

import (
	"github.com/google/uuid"
	core "github.com/iden3/go-iden3-core"
	"github.com/iden3/go-merkletree-sql"
	logger "github.com/sirupsen/logrus"
	"issuer/db"
	"issuer/service/claim"
)

type IdentityState struct {
	Claims      *Claims
	Revocations *Revocations
	Roots       *Roots
	db          *db.DB
}

func NewIdentityState(db *db.DB, treeDepth int) (*IdentityState, error) {
	logger.Debug("creating new identity state")

	claims, err := NewClaims(db, treeDepth)
	if err != nil {
		return nil, err
	}

	revs, err := NewRevocations(db, treeDepth)
	if err != nil {
		return nil, err
	}

	roots, err := NewRoots(db, treeDepth)
	if err != nil {
		return nil, err
	}

	return &IdentityState{
		Claims:      claims,
		Revocations: revs,
		Roots:       roots,
		db:          db,
	}, nil
}

func (is *IdentityState) SaveIdentity(identifier *core.ID, authClaimId uuid.UUID) error {

	id := identifier.Bytes()
	authId := []byte(authClaimId.String())

	return is.db.SaveIdentity(id, authId)

}

func (is *IdentityState) GetIdentityFromDB() (*core.ID, *uuid.UUID, error) {
	logger.Debug("IdentityState.GetIdentityFromDB() invoked")

	id, authClaimId, err := is.db.GetSavedIdentity()
	if err != nil {
		return nil, nil, err
	}

	if id == nil {
		return nil, nil, nil
	}
	coreId, err := core.IDFromBytes(id)
	if err != nil {
		return nil, nil, err
	}

	claimId, err := uuid.Parse(string(authClaimId))
	if err != nil {
		return nil, nil, err
	}

	return &coreId, &claimId, nil
}

func (is *IdentityState) AddClaimToTree(c *core.Claim) error {
	logger.Debug("IdentityState.AddClaimToTree() invoked")

	return is.Claims.SaveClaimMT(c)
}

func (is *IdentityState) AddClaimToDB(c *claim.Claim) error {
	logger.Debug("IdentityState.AddClaimToDB() invoked")

	return is.Claims.SaveClaimDB(c)
}

func (is *IdentityState) GetStateHash() (*merkletree.Hash, error) {
	logger.Debug("GetStateHash() invoked")

	return merkletree.HashElems(
		is.Claims.Tree.Root().BigInt(),
		is.Revocations.Tree.Root().BigInt(),
		is.Roots.Tree.Root().BigInt(),
	)
}

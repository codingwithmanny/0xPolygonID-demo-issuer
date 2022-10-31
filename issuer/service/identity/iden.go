package identity

import (
	"context"
	"encoding/json"
	"fmt"
	core "github.com/iden3/go-iden3-core"
	"github.com/iden3/go-iden3-crypto/babyjub"
	"github.com/iden3/go-iden3-crypto/utils"
	"github.com/iden3/go-schema-processor/verifiable"
	"github.com/pkg/errors"
	logger "github.com/sirupsen/logrus"
	"issuer/service/claim"
	issuer_contract "issuer/service/contract"
	"issuer/service/identity/state"
	"issuer/service/schema"
	"math/big"
)

type Identity struct {
	sk          babyjub.PrivateKey
	Identifier  *core.ID
	authClaimId *big.Int
	state       *state.IdentityState
	baseUrl     string
}

func New(s *state.IdentityState, sk babyjub.PrivateKey, hostUrl string) (*Identity, error) {
	iden := &Identity{
		state:   s,
		sk:      sk,
		baseUrl: hostUrl,
	}

	err := iden.init()
	if err != nil {
		return nil, fmt.Errorf("error on identitiy initialization, %v", err)
	}

	err = iden.state.SaveIdentity(iden.Identifier.String())
	return iden, err
}

func (i *Identity) init() error {
	logger.Debug("Identity.init() invoked")

	logger.Debug("setup genesis state")

	identifier, authClaim, err := i.setupGenesisState()
	if err != nil {
		return err
	}
	i.Identifier = identifier
	logger.Debugf("identity identifier: %v", i.Identifier)

	logger.Debug("generating auth claim proof")

	proof, err := i.generateProof(i.authClaimId)
	if err != nil {
		return err
	}

	authClaimModel, err := claim.CoreClaimToClaimModel(authClaim, schema.AuthBJJCredentialURL, schema.AuthBJJCredential)
	if err != nil {
		return err
	}

	claimData := make(map[string]interface{})
	claimData["x"] = i.sk.Public().X.String()
	claimData["y"] = i.sk.Public().Y.String()
	marshalledClaimData, err := json.Marshal(claimData)
	if err != nil {
		return err
	}

	authClaimModel.Data = marshalledClaimData
	authClaimModel.Issuer = i.Identifier.String()
	authClaimModel.ID = i.authClaimId.Bytes()
	authClaimModel.MTPProof = proof
	authClaimModel.Identifier = i.Identifier.String()

	logger.Debugf("adding auth claim to db, claim-id: %x", authClaimModel.ID)
	err = i.state.AddClaimToDB(authClaimModel)
	if err != nil {
		return err
	}

	return err
}

func (i *Identity) setupGenesisState() (*core.ID, *core.Claim, error) {
	logger.Trace("getting auth schema hash")

	schemaHash, err := core.NewSchemaHashFromHex(schema.AuthBJJCredentialHash)
	if err != nil {
		return nil, nil, err
	}

	logger.Trace("creating new auth claim")
	authClaim, err := claim.NewAuthClaim(i.sk.Public(), schemaHash)
	if err != nil {
		return nil, nil, err
	}

	logger.Trace("adding auth claim to the claims tree")
	err = i.state.AddClaimToTree(authClaim)
	if err != nil {
		return nil, nil, err
	}

	claimId, err := authClaim.HIndex()
	if err != nil {
		return nil, nil, err
	}
	i.authClaimId = claimId
	currState, err := i.state.GetStateHash()
	if err != nil {
		return nil, nil, err
	}

	identifier, err := core.IdGenesisFromIdenState(core.TypeDefault, currState.BigInt())
	if err != nil {
		return nil, nil, err
	}

	return identifier, authClaim, nil
}

func (i *Identity) generateProof(claimId *big.Int) ([]byte, error) {
	proof, _, err := i.state.Claims.Tree.GenerateProof(context.Background(), claimId, nil)
	if err != nil {
		return nil, err
	}

	mtProof := verifiable.Iden3SparseMerkleProof{}
	mtProof.Type = verifiable.Iden3SparseMerkleProofType
	mtProof.MTP = proof

	stateHash, err := i.state.GetStateHash()
	stateHashHex := stateHash.Hex()
	claimsRootHex := i.state.Claims.Tree.Root().Hex()
	mtProof.IssuerData = verifiable.IssuerData{
		ID: i.Identifier,
		State: verifiable.State{
			Value:          &stateHashHex,
			ClaimsTreeRoot: &claimsRootHex,
		},
	}

	proofB, err := json.Marshal(mtProof)
	if err != nil {
		return nil, err
	}

	return proofB, nil
}

func (i *Identity) AddClaim(cReq *issuer_contract.CreateClaimRequest) (*issuer_contract.CreateClaimResponse, error) {
	logger.Debug("AddClaim() invoked")

	logger.Tracef("process schema - url: %s", cReq.Schema.URL)
	slots, encodedSchema, err := schema.Process(cReq.Schema.URL, cReq.Schema.Type, cReq.Data)
	if err != nil {
		return nil, err
	}

	claimReq := &claim.CoreClaimData{
		EncodedSchema:   encodedSchema,
		Slots:           *slots,
		SubjectID:       cReq.Identifier,
		Expiration:      cReq.Expiration,
		Version:         cReq.Version,
		Nonce:           cReq.RevNonce,
		SubjectPosition: cReq.SubjectPosition,
	}

	logger.Trace("generating core-claim from the request")
	coreClaim, err := claim.GenerateCoreClaim(claimReq)
	if err != nil {
		return nil, err
	}

	claimModel, err := claim.CoreClaimToClaimModel(coreClaim, cReq.Schema.URL, cReq.Schema.Type)
	if err != nil {
		return nil, err
	}

	// set credential status
	issuerIDString := i.Identifier.String()
	cs, err := claim.CreateCredentialStatus(i.baseUrl, verifiable.SparseMerkleTreeProof, claimModel.RevNonce)
	if err != nil {
		return nil, err
	}
	claimModel.CredentialStatus = cs
	logger.Trace("finished creating the claim object from the user request")

	logger.Trace("getting auth claim")
	c, err := i.state.Claims.GetClaim(i.authClaimId.Bytes())
	if err != nil {
		return nil, err
	}

	logger.Trace("signing claim entry")
	proof, err := claim.SignClaimEntry(c, i.sign)
	if err != nil {
		return nil, err
	}

	proof.IssuerData.RevocationStatus = fmt.Sprintf("%s/api/v1/claims/revocation/status/%d", i.baseUrl, claimModel.RevNonce)

	// Save
	claimModel.Identifier = issuerIDString
	claimModel.Issuer = issuerIDString

	jsonSignatureProof, err := json.Marshal(proof)
	if err != nil {
		return nil, err
	}
	claimModel.SignatureProof = jsonSignatureProof
	claimModel.Data = cReq.Data

	logger.Trace("adding claim to the claims tree")
	err = i.state.AddClaimToTree(coreClaim)
	if err != nil {
		return nil, err
	}

	logger.Trace("adding claim to the claims DB")
	err = i.state.AddClaimToDB(claimModel)
	if err != nil {
		return nil, err
	}

	logger.Trace("saving current identity (with the updated state) to db")
	err = i.state.SaveIdentity(i.Identifier.String())
	if err != nil {
		return nil, err
	}

	return &issuer_contract.CreateClaimResponse{ID: string(claimModel.ID)}, nil
}

func (i *Identity) GetClaim(id string) (*issuer_contract.GetClaimResponse, error) {
	logger.Debug("GetClaim() invoked")

	claimModel, err := i.state.Claims.GetClaim([]byte(id))
	if err != nil {
		return nil, err
	}

	c, err := claim.ClaimModelToIden3Credential(claimModel)
	if err != nil {
		return nil, err
	}

	res := issuer_contract.GetClaimResponse(c)

	return &res, nil
}

func (i *Identity) GetIdentity() (*issuer_contract.GetIdentityResponse, error) {
	logger.Debug("GetIdentity() invoked")

	stateHash, err := i.state.GetStateHash()
	if err != nil {
		return nil, err
	}

	res := &issuer_contract.GetIdentityResponse{
		Identifier: i.Identifier.String(),
		State: &issuer_contract.IdentityState{
			Identifier:         i.Identifier.String(),
			State:              stateHash.Hex(),
			ClaimsTreeRoot:     i.state.Claims.Tree.Root().Hex(),
			RevocationTreeRoot: i.state.Revocations.Tree.Root().Hex(),
			RootOfRoots:        i.state.Roots.Tree.Root().Hex(),
		},
	}

	return res, nil
}

func (i *Identity) GetRevocationStatus(nonce uint64) (*issuer_contract.GetRevocationStatusResponse, error) {
	logger.Debug("GetRevocationStatus() invoked")

	rID := new(big.Int).SetUint64(nonce)

	res := &issuer_contract.GetRevocationStatusResponse{}
	mtp, err := i.state.Revocations.GenerateRevocationProof(rID)
	if err != nil {
		return nil, err
	}
	res.MTP = mtp
	res.Issuer.RevocationTreeRoot = i.state.Revocations.Tree.Root().Hex()
	res.Issuer.RootOfRoots = i.state.Roots.Tree.Root().Hex()
	res.Issuer.ClaimsTreeRoot = i.state.Claims.Tree.Root().Hex()

	stateHash, err := i.state.GetStateHash()
	if err != nil {
		return nil, err
	}
	res.Issuer.State = stateHash.Hex()

	return res, nil

}

// data should be a little-endian bytes representation of *big.Int.
func (i *Identity) signBytes(data []byte) ([]byte, error) {
	if len(data) > 32 {
		return nil, errors.New("data to signBytes is too large")
	}

	z := new(big.Int).SetBytes(utils.SwapEndianness(data))

	return i.sign(z)
}

func (i *Identity) sign(z *big.Int) ([]byte, error) {
	if !utils.CheckBigIntInField(z) {
		return nil, errors.New("data to signBytes is too large")
	}

	sig := i.sk.SignPoseidon(z).Compress()
	return sig[:], nil
}

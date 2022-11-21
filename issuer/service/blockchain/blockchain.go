package blockchain

import (
	"context"
	"crypto/ecdsa"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/misc"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/params"
	core "github.com/iden3/go-iden3-core"
	"github.com/iden3/go-merkletree-sql"
	"github.com/pkg/errors"
	eth "issuer/service/blockchain/contracts"
	"issuer/service/models"
	"log"
	"math"
	"math/big"
	"sync"
)

type TransitionInfo struct {
	Identifier        *core.ID
	LatestState       *merkletree.Hash
	NewState          *merkletree.Hash
	IsOldStateGenesis bool
	Proof             *models.ZKProof
}

type PublisherServer struct {
	rw              *sync.Mutex
	client          *ethclient.Client
	contractAddress common.Address
	privateKey      *ecdsa.PrivateKey
}

func (ps *PublisherServer) UpdateState(ctx context.Context, trInfo *TransitionInfo) (string, error) {
	ps.rw.Lock()
	defer ps.rw.Unlock()

	if trInfo.NewState.Equals(trInfo.LatestState) {
		return "", errors.New("state hasn't been changed")
	}

	publicKey := ps.privateKey.Public()
	publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
	if !ok {
		return "", errors.New("error casting public key to ECDSA")
	}

	fromAddress := crypto.PubkeyToAddress(*publicKeyECDSA)

	payload, err := ps.getStatePayload(trInfo)
	if err != nil {
		return "", err
	}

	tx, err := ps.sendTransaction(ctx, fromAddress, ps.contractAddress, payload)
	if err != nil {
		return "", err
	}

	return tx.Hash().Hex(), nil
}

func (ps *PublisherServer) WaitTransaction(ctx context.Context, txHash string) <-chan struct{} {
	done := make(chan struct{})

	receipt, err := ps.client.TransactionReceipt(ctx, common.HexToHash(txHash))
	if err != nil {
		log.Println("failed g")
		close(done)
	}
}

func (ps *PublisherServer) sendTransaction(ctx context.Context, from, to common.Address, payload []byte) (*types.Transaction, error) {
	nonce, err := ps.client.PendingNonceAt(ctx, from)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get nonce")
	}

	gasLimit, err := ps.client.EstimateGas(ctx, ethereum.CallMsg{
		From:  from, // the sender of the 'transaction'
		To:    &to,
		Gas:   0,             // wei <-> gas exchange ratio
		Value: big.NewInt(0), // amount of wei sent along with the call
		Data:  payload,
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to estimate gas")
	}

	latestBlockHeader, err := ps.client.HeaderByNumber(ctx, nil)
	if err != nil {
		return nil, err
	}

	baseFee := misc.CalcBaseFee(&params.ChainConfig{LondonBlock: big.NewInt(1)}, latestBlockHeader)
	b := math.Round(float64(baseFee.Int64()) * 1.25)
	baseFee = big.NewInt(int64(b))

	gasTip, err := ps.client.SuggestGasTipCap(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "failed get suggest gas tip")
	}

	maxGasPricePerFee := big.NewInt(0).Add(baseFee, gasTip)
	baseTx := &types.DynamicFeeTx{
		To:        &to,
		Nonce:     nonce,
		Gas:       gasLimit,
		Value:     big.NewInt(0),
		Data:      payload,
		GasTipCap: gasTip,
		GasFeeCap: maxGasPricePerFee,
	}

	tx := types.NewTx(baseTx)

	cid, err := ps.client.ChainID(ctx)
	if err != nil {
		return nil, err
	}

	signer := types.LatestSignerForChainID(cid)

	signedTx, err := types.SignTx(tx, signer, ps.privateKey)
	if err != nil {
		return nil, err
	}

	err = ps.client.SendTransaction(ctx, signedTx)
	if err != nil {
		return nil, err
	}

	return signedTx, nil
}

func (ps *PublisherServer) getStatePayload(ti *TransitionInfo) ([]byte, error) {
	a, b, c, err := ti.Proof.ProofToBigInts()
	if err != nil {
		return nil, err
	}
	proofA := [2]*big.Int{a[0], a[1]}
	proofB := [2][2]*big.Int{
		{b[0][1], b[0][0]},
		{b[1][1], b[1][0]},
	}
	proofC := [2]*big.Int{c[0], c[1]}

	ab, err := eth.StateMetaData.GetAbi()
	if err != nil {
		return nil, errors.WithStack(err)
	}

	payload, err := ab.Pack(
		"transitState",
		ti.Identifier.BigInt(),
		ti.LatestState.BigInt(),
		ti.NewState.BigInt(),
		ti.IsOldStateGenesis,
		proofA, proofB, proofC)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	return payload, nil
}

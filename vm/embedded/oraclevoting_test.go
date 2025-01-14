package embedded

import (
	"crypto/ecdsa"
	"github.com/idena-network/idena-go/blockchain/types"
	"github.com/idena-network/idena-go/common"
	"github.com/idena-network/idena-go/common/math"
	"github.com/idena-network/idena-go/crypto"
	"github.com/idena-network/idena-go/vm/helpers"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
	"math/big"
	"testing"
	"time"
)

type configurableOracleVotingDeploy struct {
	contractTester *contractTester
	deployStake    *big.Int

	fact                 []byte
	startTime            uint64
	votingDuration       uint64
	publicVotingDuration uint64
	winnerThreshold      byte
	quorum               byte
	committeeSize        uint64
	votingMinPayment     *big.Int
	ownerFee             byte
}

func (c *configurableOracleVotingDeploy) Parameters() (contract EmbeddedContractType, deployStake *big.Int, params [][]byte) {
	var data [][]byte
	data = append(data, c.fact)
	data = append(data, common.ToBytes(c.startTime))
	data = append(data, common.ToBytes(c.votingDuration))
	data = append(data, common.ToBytes(c.publicVotingDuration))
	data = append(data, common.ToBytes(c.winnerThreshold))
	data = append(data, common.ToBytes(c.quorum))
	data = append(data, common.ToBytes(c.committeeSize))
	if c.votingMinPayment != nil {
		data = append(data, c.votingMinPayment.Bytes())
	} else {
		data = append(data, nil)
	}
	data = append(data, common.ToBytes(c.ownerFee))
	return OracleVotingContract, c.deployStake, data
}

func (c *configurableOracleVotingDeploy) SetStartTime(startTime uint64) *configurableOracleVotingDeploy {
	c.startTime = startTime
	return c
}

func (c *configurableOracleVotingDeploy) SetVotingDuration(votingDuration uint64) *configurableOracleVotingDeploy {
	c.votingDuration = votingDuration
	return c
}

func (c *configurableOracleVotingDeploy) SetPublicVotingDuration(publicVotingDuration uint64) *configurableOracleVotingDeploy {
	c.publicVotingDuration = publicVotingDuration
	return c
}

func (c *configurableOracleVotingDeploy) SetWinnerThreshold(winnerThreshold byte) *configurableOracleVotingDeploy {
	c.winnerThreshold = winnerThreshold
	return c
}

func (c *configurableOracleVotingDeploy) SetQuorum(quorum byte) *configurableOracleVotingDeploy {
	c.quorum = quorum
	return c
}

func (c *configurableOracleVotingDeploy) SetCommitteeSize(committeeSize uint64) *configurableOracleVotingDeploy {
	c.committeeSize = committeeSize
	return c
}

func (c *configurableOracleVotingDeploy) SetVotingMinPayment(votingMinPayment *big.Int) *configurableOracleVotingDeploy {
	c.votingMinPayment = votingMinPayment
	return c
}

func (c *configurableOracleVotingDeploy) SetOwnerFee(ownerFee byte) *configurableOracleVotingDeploy {
	c.ownerFee = ownerFee
	return c
}

func (c *configurableOracleVotingDeploy) Deploy() (*oracleVotingCaller, error) {
	var err error
	if err = c.contractTester.Deploy(c); err != nil {
		return nil, err
	}
	return &oracleVotingCaller{c.contractTester}, nil
}

type oracleVotingCaller struct {
	contractTester *contractTester
}

func (c *oracleVotingCaller) StartVoting() error {
	return c.contractTester.OwnerCall(OracleVotingContract, "startVoting")
}

func (c *oracleVotingCaller) ReadProof(addr common.Address) ([]byte, error) {
	return c.contractTester.Read(OracleVotingContract, "proof", addr.Bytes())
}

func (c *oracleVotingCaller) VoteHash(vote, salt []byte) ([]byte, error) {
	return c.contractTester.Read(OracleVotingContract, "voteHash", vote, salt)
}

func (c *oracleVotingCaller) sendVoteProof(key *ecdsa.PrivateKey, vote byte, payment *big.Int) error {

	pubBytes := crypto.FromECDSAPub(&key.PublicKey)

	addr := crypto.PubkeyToAddress(key.PublicKey)

	hash := crypto.Hash(append(pubBytes, c.contractTester.ReadData("vrfSeed")...))

	v := new(big.Float).SetInt(new(big.Int).SetBytes(hash[:]))

	q := new(big.Float).Quo(v, maxHash)

	threshold, _ := helpers.ExtractUInt64(0, c.contractTester.ReadData("committeeSize"))
	shouldBeError := false
	if q.Cmp(big.NewFloat(1-float64(threshold)/float64(c.contractTester.appState.ValidatorsCache.NetworkSize()))) < 0 {
		shouldBeError = true
	}
	_, err := c.ReadProof(addr)
	if shouldBeError != (err != nil) {
		panic("assert failed")
	}

	voteHash, err := c.VoteHash(common.ToBytes(vote), addr.Bytes())
	if err != nil {
		panic(err)
	}
	return c.contractTester.Call(key, OracleVotingContract, payment, "sendVoteProof", voteHash)
}

func (c *oracleVotingCaller) sendVote(key *ecdsa.PrivateKey, vote byte, salt []byte) error {
	return c.contractTester.Call(key, OracleVotingContract, nil, "sendVote", common.ToBytes(vote), salt)
}

func (c *oracleVotingCaller) finishVoting() error {
	return c.contractTester.OwnerCall(OracleVotingContract, "finishVoting")
}

func (c *oracleVotingCaller) prolong() error {
	return c.contractTester.OwnerCall(OracleVotingContract, "prolongVoting")
}

func ConvertToInt(amount decimal.Decimal) *big.Int {
	if amount == (decimal.Decimal{}) {
		return nil
	}
	initial := decimal.NewFromBigInt(common.DnaBase, 0)
	result := amount.Mul(initial)

	return math.ToInt(result)
}

func TestOracleVoting_successScenario(t *testing.T) {
	deployContractStake := common.DnaBase

	ownerBalance := common.DnaBase

	builder := createTestContractBuilder(2000, ownerBalance)
	tester := builder.Build()
	ownerFee := byte(5)
	caller, err := tester.ConfigureDeploy(deployContractStake).OracleVoting().SetOwnerFee(ownerFee).
		SetPublicVotingDuration(4320).SetVotingDuration(4320).Deploy()
	require.NoError(t, err)

	caller.contractTester.Commit()
	caller.contractTester.setHeight(3)
	caller.contractTester.setTimestamp(30)

	contractBalance := decimal.NewFromFloat(5000.0 / 2000.0 * 99)
	caller.contractTester.SetBalance(ConvertToInt(contractBalance))

	require.Error(t, caller.StartVoting())

	contractBalance = decimal.NewFromFloat(2000)
	caller.contractTester.SetBalance(ConvertToInt(contractBalance))

	require.NoError(t, caller.StartVoting())
	caller.contractTester.Commit()

	require.Equal(t, common.ToBytes(byte(1)), caller.contractTester.ReadData("state"))
	require.Equal(t, big.NewInt(0).Quo(ConvertToInt(contractBalance), big.NewInt(20)).Bytes(), caller.contractTester.ReadData("votingMinPayment"))

	seed := types.Seed{}
	seed.SetBytes(common.ToBytes(uint64(3)))
	require.Equal(t, seed.Bytes(), caller.contractTester.ReadData("vrfSeed"))
	require.Equal(t, common.ToBytes(uint64(100)), caller.contractTester.ReadData("committeeSize"))

	// send proofs
	winnerVote := byte(1)
	votedIdentities := map[common.Address]struct{}{}
	minPayment := big.NewInt(0).SetBytes(caller.contractTester.ReadData("votingMinPayment"))

	voted := 0

	sendVoteProof := func(key *ecdsa.PrivateKey) {

		caller.contractTester.setHeight(4)

		err = caller.sendVoteProof(key, winnerVote, minPayment)
		addr := crypto.PubkeyToAddress(key.PublicKey)
		if err == nil {
			caller.contractTester.AddBalance(minPayment)
			caller.contractTester.Commit()
			require.NoError(t, err)
			if _, ok := votedIdentities[addr]; !ok {
				voted++
			}
			votedIdentities[addr] = struct{}{}
		}
	}
	for _, key := range caller.contractTester.identities {
		sendVoteProof(key)
	}

	//send votes
	for _, key := range caller.contractTester.identities {
		addr := crypto.PubkeyToAddress(key.PublicKey)

		if _, ok := votedIdentities[addr]; !ok {
			continue
		}
		caller.contractTester.setHeight(4320 * 2)

		err := caller.sendVote(key, winnerVote, addr.Bytes())
		if _, ok := votedIdentities[addr]; !ok {
			require.Error(t, err)
		} else {
			caller.contractTester.Commit()
			require.NoError(t, err)
		}
	}

	require.NoError(t, caller.finishVoting())
	caller.contractTester.Commit()

	for addr := range votedIdentities {
		b := caller.contractTester.appState.State.GetBalance(addr)
		require.Equal(t, "120652173913043478260", b.String())
	}

	stakeAfterFinish := caller.contractTester.ContractStake()
	ownerFeeAmount := big.NewInt(0).Quo(ConvertToInt(contractBalance), big.NewInt(int64(100/ownerFee)))
	require.Equal(t, deployContractStake.Bytes(), stakeAfterFinish.Bytes())

	require.Equal(t, []byte{winnerVote}, caller.contractTester.ReadData("result"))

	for addr, _ := range votedIdentities {
		require.True(t, caller.contractTester.appState.State.GetBalance(addr).Sign() == 1)
	}
	require.True(t, caller.contractTester.ContractBalance().Sign() == 0)

	caller.contractTester.setHeight(4320*2 + 4 + 30240)

	owner := caller.contractTester.ReadData("owner")

	//terminate
	dest, err := caller.contractTester.Terminate(caller.contractTester.mainKey, OracleVotingContract)
	require.NoError(t, err)
	require.Equal(t, owner, dest.Bytes())

	caller.contractTester.Commit()

	stakeToBalance := big.NewInt(0).Quo(stakeAfterFinish, big.NewInt(2))

	require.Equal(t, big.NewInt(0).Add(big.NewInt(0).Add(ownerBalance, ownerFeeAmount), stakeToBalance).String(), caller.contractTester.appState.State.GetBalance(dest).String())
	require.Nil(t, caller.contractTester.CodeHash())
	require.Nil(t, caller.contractTester.ContractStake())
	require.Nil(t, caller.contractTester.ReadData("vrfSeed"))
}

func TestOracleVoting2_Terminate_NotStartedVoting(t *testing.T) {
	deployContractStake := common.DnaBase

	ownerBalance := common.DnaBase

	builder := createTestContractBuilder(2000, ownerBalance)
	tester := builder.Build()
	ownerFee := byte(5)
	startTime := uint64(10)
	caller, err := tester.ConfigureDeploy(deployContractStake).OracleVoting().SetOwnerFee(ownerFee).
		SetPublicVotingDuration(4320).SetVotingDuration(4320).Deploy()
	require.NoError(t, err)

	caller.contractTester.Commit()

	timestamp := int64((time.Hour * 24 * 30).Seconds()) - 1 + int64(startTime)

	caller.contractTester.setTimestamp(timestamp)

	_, err = caller.contractTester.Terminate(caller.contractTester.mainKey, OracleVotingContract)
	require.Error(t, err)

	timestamp += 2
	caller.contractTester.setTimestamp(timestamp)

	_, err = caller.contractTester.Terminate(caller.contractTester.mainKey, OracleVotingContract)
	require.NoError(t, err)
}

func TestOracleVoting2_TerminateRefund(t *testing.T) {
	deployContractStake := common.DnaBase

	ownerBalance := common.DnaBase

	builder := createTestContractBuilder(2000, ownerBalance)
	tester := builder.Build()
	caller, err := tester.ConfigureDeploy(deployContractStake).OracleVoting().SetOwnerFee(10).
		SetPublicVotingDuration(4320).SetVotingDuration(4320).Deploy()
	require.NoError(t, err)

	caller.contractTester.Commit()

	contractBalance := big.NewInt(0).Mul(common.DnaBase, big.NewInt(2000))
	caller.contractTester.SetBalance(contractBalance)
	caller.contractTester.setTimestamp(21)

	require.NoError(t, caller.StartVoting())
	caller.contractTester.Commit()

	caller.contractTester.setHeight(4320*2 + 20)

	require.NoError(t, caller.prolong())
	caller.contractTester.Commit()

	require.Equal(t, common.ToBytes(byte(1)), caller.contractTester.ReadData("no-growth"))

	payment := big.NewInt(0).Mul(common.DnaBase, big.NewInt(100))

	var canVoteKeys []*ecdsa.PrivateKey

	for _, key := range caller.contractTester.identities {
		addr := crypto.PubkeyToAddress(key.PublicKey)
		if _, err := caller.ReadProof(addr); err == nil {
			canVoteKeys = append(canVoteKeys, key)
		}
	}

	sendVoteProof := func(key *ecdsa.PrivateKey, vote byte) {
		caller.contractTester.setHeight(4320*2 + 22)

		err = caller.sendVoteProof(key, vote, payment)
		if err == nil {
			caller.contractTester.AddBalance(payment)
			caller.contractTester.Commit()
			require.NoError(t, err)
		}
	}

	for i := 0; i < 5; i++ {
		sendVoteProof(canVoteKeys[i], 1)
	}
	for i := 5; i < 10; i++ {
		sendVoteProof(canVoteKeys[i], 2)
	}

	//send votes
	for i := 0; i < 5; i++ {
		addr := crypto.PubkeyToAddress(canVoteKeys[i].PublicKey)

		caller.contractTester.setHeight(4320*3 + 22)

		err := caller.sendVote(canVoteKeys[i], 1, addr.Bytes())
		require.Equal(t, "quorum is not reachable", err.Error())
	}

	caller.contractTester.setHeight(4320*2 + 20)
	require.Error(t, caller.prolong())

	caller.contractTester.setHeight(4320*4 + 20)
	require.NoError(t, caller.prolong())
	caller.contractTester.Commit()
	require.Equal(t, common.ToBytes(byte(0)), caller.contractTester.ReadData("no-growth"))

	caller.contractTester.setHeight(4320*6 + 20)
	require.NoError(t, caller.prolong())
	caller.contractTester.Commit()
	require.Equal(t, common.ToBytes(byte(1)), caller.contractTester.ReadData("no-growth"))

	caller.contractTester.setHeight(4320*8 + 20)
	require.NoError(t, caller.prolong())
	caller.contractTester.Commit()
	require.Equal(t, common.ToBytes(byte(2)), caller.contractTester.ReadData("no-growth"))

	caller.contractTester.setHeight(4320*10 + 20)
	require.NoError(t, caller.prolong())
	caller.contractTester.Commit()
	require.Equal(t, common.ToBytes(byte(3)), caller.contractTester.ReadData("no-growth"))

	caller.contractTester.setHeight(4320*12 + 20)
	require.Error(t, caller.prolong())

	caller.contractTester.setHeight(4320*12 + 4320*7 + 20)
	_, err = caller.contractTester.Terminate(caller.contractTester.mainKey, OracleVotingContract)
	caller.contractTester.Commit()
	require.NoError(t, err)

	for i := 0; i < 10; i++ {
		addr := crypto.PubkeyToAddress(canVoteKeys[i].PublicKey)
		require.Equal(t, big.NewInt(0).Mul(common.DnaBase, big.NewInt(280)).String(), caller.contractTester.appState.State.GetBalance(addr).String())
	}

	require.Nil(t, caller.contractTester.CodeHash())
	require.Nil(t, caller.contractTester.ContractStake())
	require.Nil(t, caller.contractTester.ReadData("result"))
	require.Equal(t, 0, caller.contractTester.ContractBalance().Sign())
}

func TestOracleVoting2_Refund_No_Winner(t *testing.T) {
	deployContractStake := common.DnaBase

	ownerBalance := common.DnaBase

	builder := createTestContractBuilder(2000, ownerBalance)
	tester := builder.Build()
	caller, err := tester.ConfigureDeploy(deployContractStake).OracleVoting().SetOwnerFee(0).
		SetPublicVotingDuration(4320).SetVotingDuration(4320).SetWinnerThreshold(66).Deploy()
	require.NoError(t, err)

	caller.contractTester.Commit()

	contractBalance := big.NewInt(0).Mul(common.DnaBase, big.NewInt(2000))
	caller.contractTester.SetBalance(contractBalance)
	caller.contractTester.setTimestamp(21)

	require.NoError(t, caller.StartVoting())
	caller.contractTester.Commit()

	caller.contractTester.setHeight(4320*2 + 20)

	require.NoError(t, caller.prolong())
	caller.contractTester.Commit()

	require.Equal(t, common.ToBytes(byte(1)), caller.contractTester.ReadData("no-growth"))

	payment := big.NewInt(0).Mul(common.DnaBase, big.NewInt(100))

	var canVoteKeys []*ecdsa.PrivateKey

	for _, key := range caller.contractTester.identities {
		addr := crypto.PubkeyToAddress(key.PublicKey)
		if _, err := caller.ReadProof(addr); err == nil {
			canVoteKeys = append(canVoteKeys, key)
		}
	}

	sendVoteProof := func(key *ecdsa.PrivateKey, vote byte) error {
		err = caller.sendVoteProof(key, vote, payment)
		if err == nil {
			caller.contractTester.AddBalance(payment)
			caller.contractTester.Commit()
			require.NoError(t, err)
		}
		return err
	}
	caller.contractTester.setHeight(4320*2 + 22)
	for i := 0; i < 10; i++ {
		vote := byte(1)
		if i%2 == 0 {
			vote = 2
		}
		require.NoError(t, sendVoteProof(canVoteKeys[i], vote))
	}

	caller.contractTester.setHeight(4320*4 + 20)
	require.NoError(t, caller.prolong())
	caller.contractTester.Commit()
	require.Equal(t, common.ToBytes(byte(0)), caller.contractTester.ReadData("no-growth"))

	caller.contractTester.setHeight(4320*4 + 21)

	var newCanVoteKeys []*ecdsa.PrivateKey

	for _, key := range caller.contractTester.identities {
		addr := crypto.PubkeyToAddress(key.PublicKey)
		if _, err := caller.ReadProof(addr); err == nil {
			newCanVoteKeys = append(newCanVoteKeys, key)
		}
	}

	for i := 10; i < 22; i++ {
		vote := byte(1)
		if i%2 == 0 {
			vote = 2
		}
		require.NoError(t, sendVoteProof(newCanVoteKeys[i], vote))
	}

	//send votes
	for i := 0; i < 10; i++ {
		addr := crypto.PubkeyToAddress(canVoteKeys[i].PublicKey)
		caller.contractTester.setHeight(4320*5 + 22)
		vote := byte(1)
		if i%2 == 0 {
			vote = 2
		}
		err = caller.sendVote(canVoteKeys[i], vote, addr.Bytes())
		caller.contractTester.Commit()
		require.NoError(t, err)
	}

	for i := 10; i < 19; i++ {
		addr := crypto.PubkeyToAddress(newCanVoteKeys[i].PublicKey)
		caller.contractTester.setHeight(4320*5 + 22)
		vote := byte(1)
		if i%2 == 0 {
			vote = 2
		}
		require.NoError(t, caller.sendVote(newCanVoteKeys[i], vote, addr.Bytes()))
		caller.contractTester.Commit()
	}
	caller.contractTester.setHeight(4320*6 + 22)

	require.NoError(t, caller.finishVoting())
	caller.contractTester.Commit()

	require.Equal(t, common.ToBytes(oracleVotingStateFinished), caller.contractTester.ReadData("state"))
	require.Nil(t, caller.contractTester.ReadData("result"))

	balance, _ := big.NewInt(0).SetString("221052631578947368421", 10) // ((2000 + (10+12) * payment) / 19 )*10^18

	for i := 0; i < 10; i++ {
		addr := crypto.PubkeyToAddress(canVoteKeys[i].PublicKey)
		require.Equal(t, balance.String(), caller.contractTester.appState.State.GetBalance(addr).String())
	}

	for i := 10; i < 19; i++ {
		addr := crypto.PubkeyToAddress(newCanVoteKeys[i].PublicKey)
		require.Equal(t, balance.String(), caller.contractTester.appState.State.GetBalance(addr).String())
	}

	for i := 19; i < 22; i++ {
		addr := crypto.PubkeyToAddress(newCanVoteKeys[i].PublicKey)
		require.Equal(t, "0", caller.contractTester.appState.State.GetBalance(addr).String())
	}
}

func TestOracleVoting_RewardPools(t *testing.T) {
	deployContractStake := common.DnaBase

	ownerBalance := common.DnaBase

	builder := createTestContractBuilder(2000, ownerBalance)
	tester := builder.Build()
	caller, err := tester.ConfigureDeploy(deployContractStake).OracleVoting().SetOwnerFee(0).
		SetPublicVotingDuration(4320).SetVotingDuration(4320).SetWinnerThreshold(66).Deploy()
	require.NoError(t, err)

	caller.contractTester.Commit()
	payment := big.NewInt(0).Mul(common.DnaBase, big.NewInt(200))

	contractBalance := big.NewInt(0).Mul(common.DnaBase, big.NewInt(3000))
	caller.contractTester.SetBalance(contractBalance)
	caller.contractTester.setTimestamp(21)

	require.NoError(t, caller.StartVoting())
	caller.contractTester.Commit()

	var canVoteKeys []*ecdsa.PrivateKey

	for _, key := range caller.contractTester.identities {
		addr := crypto.PubkeyToAddress(key.PublicKey)
		if _, err := caller.ReadProof(addr); err == nil {
			canVoteKeys = append(canVoteKeys, key)
		}
	}

	sendVoteProof := func(key *ecdsa.PrivateKey, vote byte) error {
		err = caller.sendVoteProof(key, vote, payment)
		if err == nil {
			caller.contractTester.AddBalance(payment)
			caller.contractTester.Commit()
			require.NoError(t, err)
		}
		return err
	}

	caller.contractTester.setHeight(22)
	for i := 0; i < 30; i++ {
		vote := byte(1)
		if i < 14 {
			vote = 2
		}
		require.NoError(t, sendVoteProof(canVoteKeys[i], vote))
	}

	pool1 := common.Address{0x1}
	pool2 := common.Address{0x2}

	for i := 0; i < 15; i++ {
		addr := crypto.PubkeyToAddress(canVoteKeys[i].PublicKey)
		caller.contractTester.appState.State.SetDelegatee(addr, pool1)
		caller.contractTester.appState.IdentityState.SetDelegatee(addr, pool1)
	}

	for i := 15; i < 20; i++ {
		addr := crypto.PubkeyToAddress(canVoteKeys[i].PublicKey)
		caller.contractTester.appState.State.SetDelegatee(addr, pool2)
		caller.contractTester.appState.IdentityState.SetDelegatee(addr, pool2)
	}
	caller.contractTester.appState.Commit(nil)

	caller.contractTester.setHeight(4320 + 22)

	//send votes
	for i := 0; i < 30; i++ {
		addr := crypto.PubkeyToAddress(canVoteKeys[i].PublicKey)
		vote := byte(1)
		if i < 14 {
			vote = 2
		}
		err = caller.sendVote(canVoteKeys[i], vote, addr.Bytes())
		caller.contractTester.Commit()
		require.NoError(t, err)
	}
	require.Equal(t, common.ToBytes(uint64(30)), caller.contractTester.ReadData("votedCount"))

	caller.contractTester.setHeight(4320*2 + 22)

	require.NoError(t, caller.finishVoting())

	events := caller.contractTester.env.Commit()
	caller.contractTester.appState.Reset()

	require.NoError(t, caller.finishVoting())
	events2 := caller.contractTester.env.Commit()

	require.Equal(t, events, events2)
	caller.contractTester.appState.Commit(nil)

	require.Equal(t, common.ToBytes(oracleVotingStateFinished), caller.contractTester.ReadData("state"))
	require.Equal(t, common.ToBytes(byte(1)), caller.contractTester.ReadData("result"))

	for i := 0; i < 20; i++ {
		addr := crypto.PubkeyToAddress(canVoteKeys[i].PublicKey)
		require.Equal(t, "0", caller.contractTester.appState.State.GetBalance(addr).String())
	}

	balance, _ := big.NewInt(0).SetString("562500000000000000000", 10) // ((200 * 30) + 3000) / 16 * 10^18

	require.Equal(t, balance.String(), caller.contractTester.appState.State.GetBalance(pool1).String())

	for i := 20; i < 30; i++ {
		addr := crypto.PubkeyToAddress(canVoteKeys[i].PublicKey)
		require.Equal(t, balance.String(), caller.contractTester.appState.State.GetBalance(addr).String())
	}

	require.Equal(t, big.NewInt(0).Mul(balance, big.NewInt(5)).String(), caller.contractTester.appState.State.GetBalance(pool2).String())
}

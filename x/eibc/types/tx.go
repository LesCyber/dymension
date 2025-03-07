package types

import (
	"encoding/hex"
	"fmt"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/x/auth/migrations/legacytx"

	sdk "github.com/cosmos/cosmos-sdk/types"

	rollapptypes "github.com/dymensionxyz/dymension/v3/x/rollapp/types"
)

var (
	_ sdk.Msg            = &MsgFulfillOrder{}
	_ sdk.Msg            = &MsgFulfillOrderAuthorized{}
	_ sdk.Msg            = &MsgUpdateDemandOrder{}
	_ legacytx.LegacyMsg = &MsgFulfillOrder{}
	_ legacytx.LegacyMsg = &MsgFulfillOrderAuthorized{}
	_ legacytx.LegacyMsg = &MsgUpdateDemandOrder{}
)

func NewMsgFulfillOrder(fulfillerAddress, orderId, expectedFee string) *MsgFulfillOrder {
	return &MsgFulfillOrder{
		FulfillerAddress: fulfillerAddress,
		OrderId:          orderId,
		ExpectedFee:      expectedFee,
	}
}

func (msg *MsgFulfillOrder) Route() string {
	return RouterKey
}

func (msg *MsgFulfillOrder) Type() string {
	return sdk.MsgTypeURL(msg)
}

func (msg *MsgFulfillOrder) GetSigners() []sdk.AccAddress {
	creator, err := sdk.AccAddressFromBech32(msg.FulfillerAddress)
	if err != nil {
		panic(err)
	}
	return []sdk.AccAddress{creator}
}

func (msg *MsgFulfillOrder) GetSignBytes() []byte {
	bz := ModuleCdc.MustMarshalJSON(msg)
	return sdk.MustSortJSON(bz)
}

func (msg *MsgFulfillOrder) ValidateBasic() error {
	err := validateCommon(msg.OrderId, msg.ExpectedFee, msg.FulfillerAddress)
	if err != nil {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, err.Error()) // TODO: join
	}
	return nil
}

func (msg *MsgFulfillOrder) GetFulfillerBech32Address() []byte {
	return sdk.MustAccAddressFromBech32(msg.FulfillerAddress)
}

func NewMsgFulfillOrderAuthorized(
	orderId,
	rollappId,
	granterAddress,
	operatorFeeAddress,
	expectedFee string,
	price sdk.Coins,
	amount math.Int,
	fulfillerFeePart math.LegacyDec,
	settlementValidated bool,
) *MsgFulfillOrderAuthorized {
	return &MsgFulfillOrderAuthorized{
		OrderId:             orderId,
		RollappId:           rollappId,
		LpAddress:           granterAddress,
		OperatorFeeAddress:  operatorFeeAddress,
		ExpectedFee:         expectedFee,
		Price:               price,
		Amount:              amount,
		OperatorFeeShare:    fulfillerFeePart,
		SettlementValidated: settlementValidated,
	}
}

func (msg *MsgFulfillOrderAuthorized) Route() string {
	return RouterKey
}

func (msg *MsgFulfillOrderAuthorized) Type() string {
	return sdk.MsgTypeURL(msg)
}

func (msg *MsgFulfillOrderAuthorized) GetSigners() []sdk.AccAddress {
	creator, err := sdk.AccAddressFromBech32(msg.LpAddress)
	if err != nil {
		panic(err)
	}
	return []sdk.AccAddress{creator}
}

func (msg *MsgFulfillOrderAuthorized) GetSignBytes() []byte {
	bz := ModuleCdc.MustMarshalJSON(msg)
	return sdk.MustSortJSON(bz)
}

func (msg *MsgFulfillOrderAuthorized) ValidateBasic() error {
	if err := validateRollappID(msg.RollappId); err != nil {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "invalid rollapp id")
	}

	if err := validateCommon(msg.OrderId, msg.ExpectedFee, msg.OperatorFeeAddress, msg.LpAddress); err != nil {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, err.Error())
	}

	if !msg.Price.IsValid() {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "price is invalid")
	}

	if msg.Amount.IsNil() || !msg.Amount.IsPositive() {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "amount is invalid")
	}

	if msg.OperatorFeeShare.IsNil() || msg.OperatorFeeShare.IsNegative() {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "operator fee share cannot be empty or negative")
	}

	if msg.OperatorFeeShare.GT(math.LegacyOneDec()) {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "operator fee share cannot be greater than 1")
	}

	return nil
}

func (msg *MsgFulfillOrderAuthorized) GetLPBech32Address() []byte {
	return sdk.MustAccAddressFromBech32(msg.LpAddress)
}

func (msg *MsgFulfillOrderAuthorized) GetOperatorFeeBech32Address() []byte {
	return sdk.MustAccAddressFromBech32(msg.OperatorFeeAddress)
}

func NewMsgUpdateDemandOrder(ownerAddr, orderId, newFee string) *MsgUpdateDemandOrder {
	return &MsgUpdateDemandOrder{
		OrderId:      orderId,
		OwnerAddress: ownerAddr,
		NewFee:       newFee,
	}
}

func (m *MsgUpdateDemandOrder) GetSigners() []sdk.AccAddress {
	creator, err := sdk.AccAddressFromBech32(m.OwnerAddress)
	if err != nil {
		panic(err)
	}
	return []sdk.AccAddress{creator}
}

func (m *MsgUpdateDemandOrder) ValidateBasic() error {
	err := validateCommon(m.OrderId, m.NewFee, m.OwnerAddress)
	if err != nil {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, err.Error())
	}

	return nil
}

func (m *MsgUpdateDemandOrder) GetSignerAddr() sdk.AccAddress {
	return sdk.MustAccAddressFromBech32(m.OwnerAddress)
}

func (m *MsgUpdateDemandOrder) GetSignBytes() []byte {
	bz := ModuleCdc.MustMarshalJSON(m)
	return sdk.MustSortJSON(bz)
}

func (m *MsgUpdateDemandOrder) Route() string {
	return RouterKey
}

func (m *MsgUpdateDemandOrder) Type() string {
	return sdk.MsgTypeURL(m)
}

func isValidOrderId(orderId string) bool {
	hashBytes, err := hex.DecodeString(orderId)
	if err != nil {
		// The string is not a valid hexadecimal string
		return false
	}
	// SHA-256 hashes are 32 bytes long
	return len(hashBytes) == 32
}

func validateCommon(orderId, fee string, address ...string) error {
	if !isValidOrderId(orderId) {
		return fmt.Errorf("%w: %s", ErrInvalidOrderID, orderId)
	}

	for _, addr := range address {
		_, err := sdk.AccAddressFromBech32(addr)
		if err != nil {
			return err
		}
	}

	feeInt, ok := math.NewIntFromString(fee)
	if !ok {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, fmt.Sprintf("parse fee: %s", fee))
	}

	if feeInt.IsNegative() {
		return ErrNegativeFee
	}

	return nil
}

func validateRollappID(rollappID string) error {
	if rollappID == "" {
		return rollapptypes.ErrInvalidRollappID
	}
	_, err := rollapptypes.NewChainID(rollappID)
	return err
}

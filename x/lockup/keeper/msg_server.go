package keeper

import (
	"context"
	"fmt"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/osmosis-labs/osmosis/v15/osmoutils"

	"github.com/dymensionxyz/dymension/v3/x/lockup/types"
)

type msgServer struct {
	keeper *Keeper
}

// NewMsgServerImpl returns an instance of MsgServer.
func NewMsgServerImpl(keeper *Keeper) types.MsgServer {
	return &msgServer{
		keeper: keeper,
	}
}

var _ types.MsgServer = msgServer{}

// LockTokens locks tokens in either two ways.
// 1. Add to an existing lock if a lock with the same owner and same duration exists.
// 2. Create a new lock if not.
// A sanity check to ensure given tokens is a single token is done in ValidateBaic.
// That is, a lock with multiple tokens cannot be created.
func (server msgServer) LockTokens(goCtx context.Context, msg *types.MsgLockTokens) (*types.MsgLockTokensResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	owner, err := sdk.AccAddressFromBech32(msg.Owner)
	if err != nil {
		return nil, err
	}

	// Charge fess for locking tokens
	if err = server.keeper.ChargeLockFee(ctx, owner, server.keeper.GetLockCreationFee(ctx), msg.Coins); err != nil {
		return nil, fmt.Errorf("charge gauge fee: %w", err)
	}

	// check if there's an existing lock from the same owner with the same duration.
	// If so, simply add tokens to the existing lock.
	lockExists := server.keeper.HasLock(ctx, owner, msg.Coins[0].Denom, msg.Duration)
	if lockExists {
		lockID, err := server.keeper.AddToExistingLock(ctx, owner, msg.Coins[0], msg.Duration)
		if err != nil {
			return nil, err
		}

		ctx.EventManager().EmitEvents(sdk.Events{
			sdk.NewEvent(
				types.TypeEvtAddTokensToLock,
				sdk.NewAttribute(types.AttributePeriodLockID, osmoutils.Uint64ToString(lockID)),
				sdk.NewAttribute(types.AttributePeriodLockOwner, msg.Owner),
				sdk.NewAttribute(types.AttributePeriodLockAmount, msg.Coins.String()),
			),
		})
		return &types.MsgLockTokensResponse{ID: lockID}, nil
	}

	// if the owner + duration combination is new, create a new lock.
	lock, err := server.keeper.CreateLock(ctx, owner, msg.Coins, msg.Duration)
	if err != nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, err.Error())
	}

	ctx.EventManager().EmitEvents(sdk.Events{
		sdk.NewEvent(
			types.TypeEvtLockTokens,
			sdk.NewAttribute(types.AttributePeriodLockID, osmoutils.Uint64ToString(lock.ID)),
			sdk.NewAttribute(types.AttributePeriodLockOwner, lock.Owner),
			sdk.NewAttribute(types.AttributePeriodLockAmount, lock.Coins.String()),
			sdk.NewAttribute(types.AttributePeriodLockDuration, lock.Duration.String()),
			sdk.NewAttribute(types.AttributePeriodLockUnlockTime, lock.EndTime.String()),
		),
	})

	return &types.MsgLockTokensResponse{ID: lock.ID}, nil
}

// BeginUnlocking begins unlocking of the specified lock.
// The lock would enter the unlocking queue, with the endtime of the lock set as block time + duration.
func (server msgServer) BeginUnlocking(goCtx context.Context, msg *types.MsgBeginUnlocking) (*types.MsgBeginUnlockingResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	lock, err := server.keeper.GetLockByID(ctx, msg.ID)
	if err != nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, err.Error())
	}

	if msg.Owner != lock.Owner {
		return nil, errorsmod.Wrap(types.ErrNotLockOwner, fmt.Sprintf("msg sender (%s) and lock owner (%s) does not match", msg.Owner, lock.Owner))
	}

	unlockingLock, err := server.keeper.BeginUnlock(ctx, lock.ID, msg.Coins)
	if err != nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, err.Error())
	}

	// N.B. begin unlock event is emitted downstream in the keeper method.

	return &types.MsgBeginUnlockingResponse{Success: true, UnlockingLockID: unlockingLock}, nil
}

func createBeginUnlockEvent(lock *types.PeriodLock) sdk.Event {
	return sdk.NewEvent(
		types.TypeEvtBeginUnlock,
		sdk.NewAttribute(types.AttributePeriodLockID, osmoutils.Uint64ToString(lock.ID)),
		sdk.NewAttribute(types.AttributePeriodLockOwner, lock.Owner),
		sdk.NewAttribute(types.AttributePeriodLockDuration, lock.Duration.String()),
		sdk.NewAttribute(types.AttributePeriodLockUnlockTime, lock.EndTime.String()),
	)
}

// ExtendLockup extends the duration of the existing lock.
// ExtendLockup would fail if the original lock's duration is longer than the new duration,
// OR if the lock is currently unlocking OR if the original lock has a synthetic lock.
func (server msgServer) ExtendLockup(goCtx context.Context, msg *types.MsgExtendLockup) (*types.MsgExtendLockupResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	owner, err := sdk.AccAddressFromBech32(msg.Owner)
	if err != nil {
		return nil, err
	}

	err = server.keeper.ExtendLockup(ctx, msg.ID, owner, msg.Duration)
	if err != nil {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, err.Error())
	}

	lock, err := server.keeper.GetLockByID(ctx, msg.ID)
	if err != nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, err.Error())
	}

	ctx.EventManager().EmitEvents(sdk.Events{
		sdk.NewEvent(
			types.TypeEvtLockTokens,
			sdk.NewAttribute(types.AttributePeriodLockID, osmoutils.Uint64ToString(lock.ID)),
			sdk.NewAttribute(types.AttributePeriodLockOwner, lock.Owner),
			sdk.NewAttribute(types.AttributePeriodLockDuration, lock.Duration.String()),
		),
	})

	return &types.MsgExtendLockupResponse{}, nil
}

// ForceUnlock ignores unlock duration and immediately unlocks the lock.
// This message is only allowed for governance-passed accounts that are kept as parameter in the lockup module.
// Locks that has been superfluid delegated is not supported.
func (server msgServer) ForceUnlock(goCtx context.Context, msg *types.MsgForceUnlock) (*types.MsgForceUnlockResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	lock, err := server.keeper.GetLockByID(ctx, msg.ID)
	if err != nil {
		return &types.MsgForceUnlockResponse{Success: false}, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, err.Error())
	}

	// check if message sender matches lock owner
	if lock.Owner != msg.Owner {
		return &types.MsgForceUnlockResponse{Success: false}, errorsmod.Wrapf(sdkerrors.ErrUnauthorized, "Sender (%s) does not match lock owner (%s)", msg.Owner, lock.Owner)
	}

	// check for chain parameter that the address is allowed to force unlock
	forceUnlockAllowedAddresses := server.keeper.GetParams(ctx).ForceUnlockAllowedAddresses
	found := false
	for _, addr := range forceUnlockAllowedAddresses {
		// defense in depth, double-checking the message owner and lock owner are both the same and is one of the allowed force unlock addresses
		if addr == lock.Owner && addr == msg.Owner {
			found = true
			break
		}
	}
	if !found {
		return &types.MsgForceUnlockResponse{Success: false}, errorsmod.Wrapf(sdkerrors.ErrUnauthorized, "Sender (%s) not allowed to force unlock", lock.Owner)
	}

	// force unlock given lock
	// This also supports the case of force unlocking lock as a whole when msg.Coins
	// provided is empty.
	err = server.keeper.PartialForceUnlock(ctx, *lock, msg.Coins)
	if err != nil {
		return &types.MsgForceUnlockResponse{Success: false}, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, err.Error())
	}

	return &types.MsgForceUnlockResponse{Success: true}, nil
}

// ChargeLockFee deducts a fee in the base denom from the specified address. The total cost is calculated as the sum
// of the fee and the amount of the base denom coin from lockCoins. If the account's balance is less than the total
// cost, the error is returned. Otherwise, the fee is charged from the payer and sent to x/txfees to be burned.
func (k Keeper) ChargeLockFee(ctx sdk.Context, payer sdk.AccAddress, fee math.Int, lockCoins sdk.Coins) (err error) {
	var feeDenom string
	if k.tk == nil {
		feeDenom, err = sdk.GetBaseDenom()
	} else {
		feeDenom, err = k.tk.GetBaseDenom(ctx)
	}
	if err != nil {
		return err
	}

	totalCost := lockCoins.AmountOf(feeDenom).Add(fee)
	accountBalance := k.bk.GetBalance(ctx, payer, feeDenom).Amount

	if accountBalance.LT(totalCost) {
		return errorsmod.Wrapf(sdkerrors.ErrInsufficientFunds, "account's balance is less than the total cost of the message: balance: %s%s, total cost: %s%s", accountBalance, feeDenom, totalCost, feeDenom)
	}

	return k.tk.ChargeFeesFromPayer(ctx, payer, sdk.NewCoin(feeDenom, fee), nil)
}

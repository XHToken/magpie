package routerservice

import (
	"errors"
	"fmt"
	"strings"

	"github.com/yetone/magpie/internal/routercloud"
	"github.com/yetone/magpie/internal/routerredact"
)

func redactionHistoryError(err error) error {
	var missing *routerredact.HistoryRequired
	if errors.As(err, &missing) {
		return &routercloud.Error{Code: "redaction_history_prepare_required", Message: fmt.Sprintf("Local history preparation required: %d user messages, approximately %d characters. No cloud attempt started.", missing.Messages, missing.Characters), Status: 409}
	}
	var failure *routerredact.Failure
	if errors.As(err, &failure) && (strings.HasPrefix(failure.Code, "redaction_store_") || strings.HasPrefix(failure.Code, "redaction_outbound_") || failure.Code == "redaction_placeholder_collision") {
		return &routercloud.Error{Code: routerredact.SafeFailureCode(failure.Code), Message: "Private redaction preparation blocked; inspect local recognition storage and history.", Status: 503}
	}
	return err
}

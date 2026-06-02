package services

import (
	"errors"
	"testing"

	"github.com/lib/pq"
)

func TestIsUndefinedColumnErrorMatchesPQUndefinedColumn(t *testing.T) {
	err := &pq.Error{
		Code:    "42703",
		Message: `column "daily_stock_analysis_user_id" does not exist`,
	}

	if !isUndefinedColumnError(err, "daily_stock_analysis_user_id") {
		t.Fatal("expected undefined-column pq error to match")
	}
}

func TestIsUndefinedColumnErrorRejectsOtherErrors(t *testing.T) {
	if isUndefinedColumnError(errors.New("random error"), "daily_stock_analysis_user_id") {
		t.Fatal("expected non-pq error to be rejected")
	}

	err := &pq.Error{
		Code:    "42703",
		Message: `column "other_column" does not exist`,
	}
	if isUndefinedColumnError(err, "daily_stock_analysis_user_id") {
		t.Fatal("expected different missing column to be rejected")
	}
}

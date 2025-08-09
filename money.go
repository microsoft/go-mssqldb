package mssql

import (
	"github.com/shopspring/decimal"
)

type Money struct {
	decimal.Decimal
}

type NullMoney struct {
	decimal.NullDecimal
}

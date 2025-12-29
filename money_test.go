package mssql

import (
	"database/sql"
	"encoding/binary"
	"testing"

	"github.com/microsoft/go-mssqldb/internal/decimal"
	shopspring "github.com/shopspring/decimal"
)

func TestBulkInvalidString(t *testing.T) {
	t.Parallel()

	b := &Bulk{}

	col := columnStruct{
		ti: typeInfo{
			TypeId: typeMoneyN,
			Size:   8,
		},
	}

	_, err := b.makeParam("bulk", col)

	if err == nil {
		t.Error("error expected")
	}
}

func TestBulkInvalidType(t *testing.T) {
	t.Parallel()

	b := &Bulk{}

	col := columnStruct{
		ti: typeInfo{
			TypeId: typeMoneyN,
			Size:   8,
		},
	}

	_, err := b.makeParam(12345, col)

	if err == nil {
		t.Error("error expected")
	}
}

func TestBulkMoneyN(t *testing.T) {
	t.Parallel()

	b := &Bulk{}

	col := columnStruct{
		ti: typeInfo{
			TypeId: typeMoneyN,
			Size:   8,
		},
	}

	res, err := b.makeParam("-882342757768.9998", col)

	if err != nil {
		t.Errorf("unexpected error: %s", err.Error())
	}

	m := readMoney(res.buffer)
	if m != -8823427577689998 {
		t.Errorf("wrong money value: %s", decimal.Int64ToDecimalScale(m, 4).String())
	}
}

func TestBulkMoneyPositive(t *testing.T) {
	t.Parallel()

	b := &Bulk{}

	col := columnStruct{
		ti: typeInfo{
			TypeId: typeMoney,
			Size:   8,
		},
	}

	res, err := b.makeParam("882342757768.9998", col)

	if err != nil {
		t.Errorf("unexpected error: %s", err.Error())
	}

	m := readMoney(res.buffer)
	if m != 8823427577689998 {
		t.Errorf("wrong money value: %s", decimal.Int64ToDecimalScale(m, 4).String())
	}
}

func TestBulkMoneyNegative(t *testing.T) {
	t.Parallel()

	b := &Bulk{}

	col := columnStruct{
		ti: typeInfo{
			TypeId: typeMoney,
			Size:   8,
		},
	}

	res, err := b.makeParam("-882342757768.9998", col)

	if err != nil {
		t.Errorf("unexpected error: %s", err.Error())
	}

	m := readMoney(res.buffer)
	if m != -8823427577689998 {
		t.Errorf("wrong money value: %s", decimal.Int64ToDecimalScale(m, 4).String())
	}
}

func TestBulkMoney4Positive(t *testing.T) {
	t.Parallel()

	b := &Bulk{}

	col := columnStruct{
		ti: typeInfo{
			TypeId: typeMoney4,
			Size:   4,
		},
	}

	res, err := b.makeParam("182342.9998", col)

	if err != nil {
		t.Errorf("unexpected error: %s", err.Error())
	}

	m := readSmallMoney(res.buffer)
	if m != 1823429998 {
		t.Errorf("wrong money value: %s", decimal.Int64ToDecimalScale(m, 4).String())
	}
}

func TestBulkMoney4Negative(t *testing.T) {
	t.Parallel()

	b := &Bulk{}

	col := columnStruct{
		ti: typeInfo{
			TypeId: typeMoney4,
			Size:   4,
		},
	}

	res, err := b.makeParam("-182342.9998", col)

	if err != nil {
		t.Errorf("unexpected error: %s", err.Error())
	}

	m := readSmallMoney(res.buffer)
	if m != -1823429998 {
		t.Errorf("wrong money value: %s", decimal.Int64ToDecimalScale(m, 4).String())
	}
}

func TestMoneyNullDecimal(t *testing.T) {
	t.Parallel()

	s := &Stmt{}

	res, err := s.makeParam(
		Money[shopspring.NullDecimal]{
			shopspring.NewNullDecimal(shopspring.New(-287813234234, -4)),
		},
	)

	if err != nil {
		t.Errorf("unexpected error: %s", err.Error())
	}

	if res.ti.TypeId != typeNVarChar {
		t.Errorf("wrong type value: %d", typeNVarChar)
	}

	u, _ := ucs22str(res.buffer)
	if u != "-28781323.4234" {
		t.Errorf("wrong money value: %#v", res.buffer)
	}
}

func TestMoneyNullDecimalNull(t *testing.T) {
	t.Parallel()

	s := &Stmt{}

	res, err := s.makeParam(
		Money[shopspring.NullDecimal]{
			shopspring.NullDecimal{},
		},
	)

	if err != nil {
		t.Errorf("unexpected error: %s", err.Error())
	}

	if res.ti.TypeId != typeNVarChar {
		t.Errorf("wrong type value: %d", typeNVarChar)
	}

	if len(res.buffer) != 0 {
		t.Errorf("wrong buffer size value: %d", res.buffer)
	}
}

func TestMoneyDecimal(t *testing.T) {
	t.Parallel()

	s := &Stmt{}

	res, err := s.makeParam(Money[shopspring.Decimal]{
		shopspring.New(-82913823232, -4),
	},
	)

	if err != nil {
		t.Errorf("unexpected error: %s", err.Error())
	}

	if res.ti.TypeId != typeNVarChar {
		t.Errorf("wrong type value: %d", typeNVarChar)
	}

	u, _ := ucs22str(res.buffer)
	if u != "-8291382.3232" {
		t.Errorf("wrong money value: %#v", res.buffer)
	}
}

func TestMoneyOutNullDecimal(t *testing.T) {
	t.Parallel()

	s := &Stmt{}

	res, err := s.makeParam(sql.Out{
		Dest: Money[shopspring.NullDecimal]{
			shopspring.NewNullDecimal(shopspring.New(-287813234234, -4)),
		},
	})

	if err != nil {
		t.Errorf("unexpected error: %s", err.Error())
	}

	if res.ti.Size != 8 {
		t.Errorf("wrong size value: %d", res.ti.Size)
	}

	m := readMoney(res.buffer)
	if m != -287813234234 {
		t.Errorf("wrong money value: %s", decimal.Int64ToDecimalScale(m, 4).String())
	}
}

func TestMoneyOutNullDecimalNull(t *testing.T) {
	t.Parallel()

	s := &Stmt{}

	res, err := s.makeParam(sql.Out{
		Dest: Money[shopspring.NullDecimal]{
			shopspring.NullDecimal{},
		},
	})

	if err != nil {
		t.Errorf("unexpected error: %s", err.Error())
	}

	if len(res.buffer) != 0 {
		t.Errorf("wrong buffer size value: %d", res.buffer)
	}
}

func TestMoneyOutDecimal(t *testing.T) {
	t.Parallel()

	s := &Stmt{}

	res, err := s.makeParam(sql.Out{
		Dest: Money[shopspring.Decimal]{
			shopspring.New(-82913823232, -4),
		},
	})

	if err != nil {
		t.Errorf("unexpected error: %s", err.Error())
	}

	if res.ti.Size != 8 {
		t.Errorf("wrong size value: %d", res.ti.Size)
	}

	m := readMoney(res.buffer)
	if m != -82913823232 {
		t.Errorf("wrong money value: %s", decimal.Int64ToDecimalScale(m, 4).String())
	}
}

func TestMoneyOutDecimalBetterPrecision(t *testing.T) {
	t.Parallel()

	s := &Stmt{}

	res, err := s.makeParam(sql.Out{
		Dest: Money[shopspring.Decimal]{
			shopspring.New(-82913823232, -6),
		},
	})

	if err != nil {
		t.Errorf("unexpected error: %s", err.Error())
	}

	if res.ti.Size != 8 {
		t.Errorf("wrong size value: %d", res.ti.Size)
	}

	m := readMoney(res.buffer)
	if m != -829138232 {
		t.Errorf("wrong money value: %s", decimal.Int64ToDecimalScale(m, 4).String())
	}
}

func TestMoneyValueDecimal(t *testing.T) {
	d := shopspring.New(-82913823232, -6)

	m := Money[shopspring.Decimal]{d}

	dv, _ := d.Value()
	mv, err := m.Value()

	if mv != dv {
		t.Errorf("wrong money Value(): %#v, must be: %#v", mv, dv)
	}

	if err != nil {
		t.Errorf("unexpected money Value() error: %s", err.Error())
	}
}

func TestMoneyScanDecimal(t *testing.T) {
	v := "123323.3233"

	d := &shopspring.Decimal{}
	m := &Money[shopspring.Decimal]{}

	d.Scan(v)
	m.Scan(v)

	if !m.Decimal.Equal(*d) {
		t.Errorf("wrong money Scan() result: %#v, must be: %#v", m.Decimal, *d)
	}
}

func TestMoneyValueNullDecimal(t *testing.T) {
	nd := shopspring.NewNullDecimal(shopspring.New(-82913823232, -6))

	m := Money[shopspring.NullDecimal]{nd}

	dv, _ := nd.Value()
	mv, err := m.Value()

	if mv != dv {
		t.Errorf("wrong money Value(): %#v, must be: %#v", mv, dv)
	}

	if err != nil {
		t.Errorf("unexpected money Value() error: %s", err.Error())
	}
}

func TestMoneyScanNullDecimal(t *testing.T) {
	v := "123323.3233"

	nd := &shopspring.NullDecimal{}
	nm := &Money[shopspring.NullDecimal]{}

	nd.Scan(v)
	nm.Scan(v)

	if !nm.Decimal.Decimal.Equal(nd.Decimal) {
		t.Errorf("wrong money Scan() result: %#v, must be: %#v", nm.Decimal.Decimal, nd.Decimal)
	}
}

func readMoney(buf []byte) int64 {
	return int64((uint64(binary.LittleEndian.Uint32(buf)) << 32) | uint64(binary.LittleEndian.Uint32(buf[4:])))
}

func readSmallMoney(buf []byte) int64 {
	return int64(int32(binary.LittleEndian.Uint32(buf)))
}

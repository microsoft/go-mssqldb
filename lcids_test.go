package mssql

import (
	"bytes"
	"database/sql"
	"encoding/binary"
	"testing"
)

// SQL that generates other SQLs for running comparisons.
// It does not create any persisent database objects,
// employing CTEs instead. The structure of CTEs is pretty
// self-explanatory, with one CTE per code page containing
// all available LCIDs. The exception is code page 1252, which
// contains all LCIDs not included in all other CTEs.
//
// There are some test integrity assertions that:
//  1. Ensure that all LCIDs in the system (as returned by sys.fn_helpcollations())
//     are covered by the test.
//  2. The 1252 CTE does not return any "leaked" code pages that are not 1252.
//  3. All code pages come with sample reference data for fetching comparison.
//
// None of the SQL syntax uses any SQL Server version-specific syntax,
// and should work on any version without change.
const comparisonQueriesGeneratorSQL = `
--
-- Generates SELECT statements for fetching data encoded with various code pages
-- along with the same data encoded in UTF-16.
-- The client can execute the generated queries and compare "codepage_data" column
-- with the original data returned in the "reference_data" column.
--
-- Single-byte code pages contain the entire range of 128-255 byte values as sample data.
-- Double-byte code pages contain a representative sample text since it is not practical
-- to provide the entire range of possible characters.
--
-- The format of the query is
--    SELECT
--        N'<sample data>' AS [reference_data]
--      , CAST(N'<sample data> COLLATE <collation with the given code page and lcid> AS VARCHAR(1000)) AS [codepage_data]
-- This way the conversion from Nvarchar to Varchar does not depend on the collation
-- of the currently active database.
--
-- The dirver's job is to fetch both columns and compare them. If the codepage/LCID mapping is done wrong,
-- the comparison will fail.
--
with
cte_data (cp, datasample) as (
  -- Thai
  select  874, N'€…‘’“”•–—กขฃคฅฆงจฉชซฌฎฏฐฑฒณดตถทธนบปผฝพฟภมยรฤลฦวศษสหฬอฮฯะัาำิีึืฺุู฿เแโใไๅๆ็่้๊๋์ํ๎๏๐๑๒๓๔๕๖๗๘๙๚๛'
  union all
  -- Japanese (double-byte encoding, so provide a sample rather than the entire range)
  select  932, N'産業通商資源部の安徳根（アン・ドクグン）長官は「今後もモバイル・ワールド・コングレス（ＭＷＣ）など海外の見本市で統合韓国館を拡大し、参加企業の成果を高める」との方針を示した。'
  union all
  -- Chinese Simplified (double-byte encoding, so provide a sample rather than the entire range)
  select  936, N'乘坐“蓝梦之星”号邮轮访问济州的中国团体游客造访新罗免税店济州分店。'
  union all
  -- Chinese Traditional (double-byte encoding, so provide a sample rather than the entire range)
  select  950, N'首相弗雷澤里克森在首相府和國會所在地克里斯蒂安堡宮的露台上，向民眾公布王儲正式登基成為國王。'
  union all
  -- Korean (double-byte encoding, so provide a sample rather than the entire range)
  select  949, N'홍성은 마늘과 한돈, 김 등 산지로 유명하지만, 그동안 상대적으로 시설원예 분야에서는 취약하다는 평가를 받았다.'
  union all
  -- Central European (Czech, Slovak, Polish, Hungarian, etc.)
  select 1250, N'€‚„…†‡‰Š‹ŚŤŽŹ‘’“”•–—™š›śťžźˇ˘Ł¤Ą¦§¨©Ş«¬®Ż°±˛ł´µ¶·¸ąş»Ľ˝ľżŔÁÂĂÄĹĆÇČÉĘËĚÍÎĎĐŃŇÓÔŐÖ×ŘŮÚŰÜÝŢßŕáâăäĺćçčéęëěíîďđńňóôőö÷řůúűüýţ˙'
  union all
  -- Cyrillic
  select 1251, N'ЂЃ‚ѓ„…†‡€‰Љ‹ЊЌЋЏђ‘’“”•–—™љ›њќћџЎўЈ¤Ґ¦§Ё©Є«¬®Ї°±Ііґµ¶·ё№є»јЅѕїАБВГДЕЖЗИЙКЛМНОПРСТУФХЦЧШЩЪЫЬЭЮЯабвгдежзийклмнопрстуфхцчшщъыьэюя'
  union all
  -- Generic Latin
  select 1252, N'€‚ƒ„…†‡ˆ‰Š‹ŒŽ‘’“”•–—˜™š›œžŸ¡¢£¤¥¦§¨©ª«¬®¯°±²³´µ¶·¸¹º»¼½¾¿ÀÁÂÃÄÅÆÇÈÉÊËÌÍÎÏÐÑÒÓÔÕÖ×ØÙÚÛÜÝÞßàáâãäåæçèéêëìíîïðñòóôõö÷øùúûüýþÿ'
  union all
  -- Greek
  select 1253, N'€‚ƒ„…†‡‰‹‘’“”•–—™›΅Ά£¤¥¦§¨©«¬®―°±²³΄µ¶·ΈΉΊ»Ό½ΎΏCxΐΑΒΓΔΕΖΗΘΙΚΛΜΝΞΟΠΡΣΤΥΦΧΨΩΪΫάέήίΰαβγδεζηθικλμνξοπρςστυφχψωϊϋόύώ'
  union all
  -- Turkish
  select 1254, N'€‚ƒ„…†‡ˆ‰Š‹Œ‘’“”•–—˜™š›œŸ¡¢£¤¥¦§¨©ª«¬®¯°±²³´µ¶·¸¹º»¼½¾¿ÀÁÂÃÄÅÆÇÈÉÊËÌÍÎÏĞÑÒÓÔÕÖ×ØÙÚÛÜİŞßàáâãäåæçèéêëìíîïğñòóôõö÷øùúûüışÿ'
  union all
  -- Hebrew
  select 1255, N'€‚ƒ„…†‡ˆ‰‹‘’“”•–—˜™›¡¢£₪¥¦§¨©×«¬®¯°±²³´µ¶·¸¹÷»¼½¾¿ְֱֲֳִֵֶַָֻּֽ־ֿ׀ׁׂ׃װױײ׳״אבגדהוזחטיךכלםמןנסעףפץצקרשת'
  union all
  -- Arabic
  select 1256, N'€پ‚ƒ„…†‡ˆ‰ٹ‹Œچژڈگ‘’“”•–—ک™ڑ›œں،¢£¤¥¦§¨©ھ«¬®¯°±²³´µ¶·¸¹؛»¼½¾؟ہءآأؤإئابةتثجحخدذرزسشصض×طظعغـفقكàلâمنهوçèéêëىيîô÷ùûüے'
  union all
  -- Baltic countries (Estonia, Latvia, Lithuania)
  select 1257, N'€‚„…†‡‰‹¨ˇ¸‘’“”•–—™›¯˛¢£¤¦§Ø©Ŗ«¬®Æ°±²³´µ¶·ø¹ŗ»¼½¾æĄĮĀĆÄÅĘĒČÉŹĖĢĶĪĻŠŃŅÓŌÕÖ×ŲŁŚŪÜŻŽßąįāćäåęēčéźėģķīļšńņóōõö÷ųłśūüżž˙'
  union all
  -- Vietnamese
  select 1258, N'€‚ƒ„…†‡ˆ‰‹Œ‘’“”•–—˜™›œŸ¡¢£¤¥¦§¨©ª«¬®¯°±²³´µ¶·¸¹º»¼½¾¿ÀÁÂĂÄÅÆÇÈÉÊËÍÎÏĐÑÓÔƠÖ×ØÙÚÛÜƯßàáâăäåæçèéêëíîïđñóôơö÷øùúûüư₫ÿ'
),
cte_cp874 (cp, lcid, collation) as (
            select 874, 0x0000041e, N'Thai_100_BIN2'
),
cte_cp932 (cp, lcid, collation) as (
            select 932, 0x00000411, N'Japanese_XJIS_100_BIN2'
  union all select 932, 0x00010411, N'Japanese_Unicode_BIN2'
  union all select 932, 0x00040411, N'Japanese_Bushu_Kakusu_100_BIN2'
),
cte_cp936 (cp, lcid, collation) as (
            select 936, 0x00000804, N'Chinese_Simplified_Pinyin_100_BIN2'
  union all select 936, 0x00020804, N'Chinese_Simplified_Stroke_Order_100_BIN2'
),
cte_cp949 (cp, lcid, collation) as (
            select 949, 0x00000412, N'Korean_100_BIN2'
),
cte_cp950 (cp, lcid, collation) as (
            select 950, 0x00000404, N'Chinese_Traditional_Stroke_Count_100_BIN2'
  union all select 950, 0x00001404, N'Chinese_Traditional_Pinyin_100_BIN2'
  union all select 950, 0x00000c04, N'Chinese_Hong_Kong_Stroke_90_BIN2'
  union all select 950, 0x00030404, N'Chinese_Traditional_Bopomofo_100_BIN2'
  union all select 950, 0x00021404, N'Chinese_Traditional_Stroke_Order_100_BIN2'
),
cte_cp1250 (cp, lcid, collation) as (
            select 1250, 0x0000041c, N'Albanian_100_BIN2'
  union all select 1250, 0x0000041a, N'Croatian_100_BIN2'
  union all select 1250, 0x00000405, N'Czech_100_BIN2'
  union all select 1250, 0x0000040e, N'Hungarian_100_BIN2'
  union all select 1250, 0x00000415, N'Polish_100_BIN2'
  union all select 1250, 0x00000418, N'Romanian_100_BIN2'
  union all select 1250, 0x0000041b, N'Slovak_100_BIN2'
  union all select 1250, 0x00000424, N'Slovenian_100_BIN2'
  union all select 1250, 0x0001040e, N'Hungarian_Technical_100_BIN2'
  union all select 1250, 0x00000442, N'Turkmen_100_BIN2'
  union all select 1250, 0x0000081A, N'Serbian_Latin_100_BIN2'
  union all select 1250, 0x0000141A, N'Bosnian_Latin_100_BIN2'
),
cte_cp1251 (cp, lcid, collation) as (
            select 1251, 0x0000042f, N'Macedonian_FYROM_100_BIN2'
  union all select 1251, 0x00000419, N'Cyrillic_General_100_BIN2'
  union all select 1251, 0x00000c1a, N'Serbian_Cyrillic_100_BIN2'
  union all select 1251, 0x00000422, N'Ukrainian_100_BIN2'
  union all select 1251, 0x0000043f, N'Kazakh_100_BIN2'
  union all select 1251, 0x00000444, N'Tatar_100_BIN2'
  union all select 1251, 0x0000082c, N'Azeri_Cyrillic_100_BIN2'
  union all select 1251, 0x0000046D, N'Bashkir_100_BIN2'
  union all select 1251, 0x00000485, N'Yakut_100_BIN2'
  union all select 1251, 0x0000201A, N'Bosnian_Cyrillic_100_BIN2'
),
cte_cp1253 (cp, lcid, collation) as (
            select 1253, 0x00000408, N'Greek_100_BIN2'
),
cte_cp1254 (cp, lcid, collation) as (
            select 1254, 0x0000041f, N'Turkish_100_BIN2'
  union all select 1254, 0x0000042c, N'Azeri_Latin_100_BIN2'
  union all select 1254, 0x00000443, N'Uzbek_Latin_100_BIN2'
),
cte_cp1255 (cp, lcid, collation) as (
            select 1255, 0x0000040d, N'Hebrew_100_BIN2'
),
cte_cp1256 (cp, lcid, collation) as (
            select 1256, 0x00000401, N'Arabic_100_BIN2'
  union all select 1256, 0x00000429, N'Persian_100_BIN2'
  union all select 1256, 0x00000420, N'Urdu_100_BIN2'
  union all select 1256, 0x00000480, N'Uighur_100_BIN2'
  union all select 1256, 0x0000048C, N'Dari_100_BIN2'
),
cte_cp1257 (cp, lcid, collation) as (
            select 1257, 0x00000425, N'Estonian_100_BIN2'
  union all select 1257, 0x00000426, N'Latvian_100_BIN2'
  union all select 1257, 0x00000427, N'Lithuanian_100_BIN2'
),
cte_cp1258 (cp, lcid, collation) as (
            select 1258, 0x0000042a, N'Vietnamese_100_BIN2'
),
cte_cp_non1252 (cp, lcid, collation) as (
            select cp, lcid, collation from cte_cp874
  union all select cp, lcid, collation from cte_cp932
  union all select cp, lcid, collation from cte_cp936
  union all select cp, lcid, collation from cte_cp949
  union all select cp, lcid, collation from cte_cp950
  union all select cp, lcid, collation from cte_cp1250
  union all select cp, lcid, collation from cte_cp1251
  union all select cp, lcid, collation from cte_cp1253
  union all select cp, lcid, collation from cte_cp1254
  union all select cp, lcid, collation from cte_cp1255
  union all select cp, lcid, collation from cte_cp1256
  union all select cp, lcid, collation from cte_cp1257
  union all select cp, lcid, collation from cte_cp1258
),
cte_cp1252 (cp, lcid, collation) as (
  select distinct
      -- TEST INTEGRITY ASSERTION:
      -- If there's any code page that is not 1252 - cause a failure.
      -- Use "divide by zero" as a distinctive error for this failure point.
      iif(collationproperty(name, 'codepage') = 1252, 1252, 1/0)
    , cast(collationproperty(name, 'lcid') as binary(4))
      -- Doesn't matter which collation to pick as long as it's the only one for the given codepage/lcid combo.
    , max (name) over (partition by collationproperty(name, 'codepage'), collationproperty(name, 'lcid'))
  from fn_helpcollations() hc
  where
      collationproperty(name, 'codepage') not in (0, 65001)
  and collationproperty(name, 'sortid') = 0
  and not exists (
  select * from cte_cp_non1252
  where
        cp = collationproperty(hc.name, 'codepage')
    and lcid = cast(collationproperty(hc.name, 'lcid') as binary(4)))
),
cte_cp_all (cp, lcid, collation) as (
            select cp, lcid, collation from cte_cp_non1252
  union all select cp, lcid, collation from cte_cp1252
),
cte_sqltext (cp, lcid, collation, sqltext) as (
select
    cte_cp_all.cp
  , cte_cp_all.lcid
  , cte_cp_all.collation
  , N'select' + char(13) + char(10) +
    N'  N''' + cte_data.datasample + N''' as reference_data' + nchar(13) + nchar(10) +
    N', cast(N''' + cte_data.datasample + N''' collate ' + cte_cp_all.collation + N' as varchar(1000)) as codepage_data'
from
  cte_cp_all left join cte_data on (cte_cp_all.cp = cte_data.cp)
)
select cp, lcid, collation, sqltext from cte_sqltext
union all
select
    -- TEST INTEGRITY ASSERTION:
    -- Check if all codepages have a sample data associated with them.
    -- The "sqltext" property will be set to NULL for those that do not.
    -- Cause a failure if found.
    -- Use "arithmetic overflow" as a distinctive error for this failure point
    -- by casting the code page value (that is greater than 255) to tinyint.
    cast(cp as tinyint), null, null, null
from cte_sqltext
where sqltext is null
union all
select
    -- TEST INTEGRITY ASSERTION:
    -- Check for "orphan" codepage/lcid combos that are present in SQL server
    -- but not covered by the above CTEs. Exclude:
    --   - SQL collations (the ones whose SortId is nonzero)
    --   - Unicode-only collations (the ones that do not have a code page)
    --   - UTF-8 collations - those are orthogonal to LCIDs and don't need to be tested.
    -- If found such orphan combos - cause a failure.
    -- Use "invalid cast" as a distinctive error for this failure point by
    -- casting collation name (that is guaranteed to not be a valid number) to a number.
    cast(name as int), null, null, null
from fn_helpcollations() hc
where
      collationproperty(name, 'codepage') not in (0, 65001)
  and collationproperty(name, 'sortid') = 0
  and not exists (
    select * from cte_cp_all where
        cast(collationproperty(hc.name, 'lcid') as binary(4)) = cte_cp_all.lcid
    and collationproperty(hc.name, 'codepage') = cte_cp_all.cp
  )
`

// Represents codepage/LCID pair
type CpLcid struct {
	cp   int
	lcid int32
}

// Represents a collation / comparison SQL text data
// for each given codepage/LCID pair.
type CpLcidComparisonData struct {
	collation string
	sqltext   string
}

// Type alias for the mapping of codepage/LCID pair
// to its collation/sqltext data.
type CpLcidComparisonMap map[CpLcid]CpLcidComparisonData

// Builds a map of LCID fetching queries for all codepage/LCID pairs.
func buildLcidFetchComparisonMap(conn *sql.DB, t *testing.T) CpLcidComparisonMap {
	stmt, err := conn.Prepare(comparisonQueriesGeneratorSQL)
	if err != nil {
		t.Error("Unable to run comparison queries generator query", err.Error())
	}
	defer stmt.Close()

	rows, err := stmt.Query()
	if err != nil {
		t.Error("Query failed:", err.Error())
	}
	defer rows.Close()

	result := make(CpLcidComparisonMap)

	for rows.Next() {
		var codepage int
		var lcidRaw []byte = make([]byte, 4)
		var collation string
		var sqltext string

		err := rows.Scan(&codepage, &lcidRaw, &collation, &sqltext)
		if err != nil {
			t.Error("Failed to fetch the comparison SQL text row:", err.Error())
		}

		var lcid int32
		err = binary.Read(bytes.NewReader(lcidRaw), binary.BigEndian, &lcid)
		if err != nil {
			t.Error("Failed to convert LCID from binary to int:", err.Error())
		}

		cplcid := CpLcid{codepage, lcid}
		cplciddata := CpLcidComparisonData{collation, sqltext}

		result[cplcid] = cplciddata
	}

	err = rows.Err()
	if err != nil {
		t.Error("Rows containing comparison queries have errors", err)
	}

	return result
}

// Verifies a specific LCID fetch by comparing it to its reference data.
func verifyLcidFetch(conn *sql.DB, sqltext *string, t *testing.T) bool {
	var refdata string
	var cpdata string

	err := conn.QueryRow(*sqltext).Scan(&refdata, &cpdata)
	if err != nil {
		t.Error("Cannot scan reference and codepage data", err)
	}

	return refdata == cpdata
}

// Tests the fetching of all available LCIDs to verify that they
// are being correctly mapped to their respective Windows code pages.
func TestLcidsFetching(t *testing.T) {
	conn, _ := sql.Open("sqlserver", makeConnStr(t).String())
	defer conn.Close()

	cplcidmap := buildLcidFetchComparisonMap(conn, t)

	success := true
	for cplcid, cplciddata := range cplcidmap {
		if !verifyLcidFetch(conn, &cplciddata.sqltext, t) {
			success = false
			t.Logf("LCID fetch failed for codepage %d, lcid 0x%x, collation %s",
				cplcid.cp, cplcid.lcid, cplciddata.collation)
		}
	}

	if !success {
		t.Error("There are failed LCID fetches. See test log for the details.")
	}
}

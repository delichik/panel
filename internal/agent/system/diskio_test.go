package system

import (
	"strings"
	"testing"
)

func TestParseDiskStatsSumsWholeDisksOnly(t *testing.T) {
	raw := `   8       0 sda 100 0 2000 10 50 0 4000 20 0 0 0
   8       1 sda1 100 0 2000 10 50 0 4000 20 0 0 0
   8      16 sdb 1 0 8 0 1 0 16 0 0 0 0
   7       0 loop0 5 0 5 0 0 0 0 0 0 0 0
   1       0 ram0 7 0 7 0 0 0 0 0 0 0 0
 253       0 zram0 9 0 9 0 0 0 0 0 0 0 0
`
	allowed := map[string]struct{}{"sda": {}, "sdb": {}}
	totals, err := parseDiskStats(strings.NewReader(raw), allowed)
	if err != nil {
		t.Fatal(err)
	}
	// sda+sdb: (2000+8)*512 读，(4000+16)*512 写；分区 sda1 与 loop/ram/zram 排除。
	wantRead := int64(2000+8) * diskSectorSize
	wantWrite := int64(4000+16) * diskSectorSize
	if totals.readBytes != wantRead || totals.writeBytes != wantWrite {
		t.Fatalf("parseDiskStats = (%d, %d), want (%d, %d)", totals.readBytes, totals.writeBytes, wantRead, wantWrite)
	}
}

func TestParseDiskStatsSkipsMalformedLines(t *testing.T) {
	raw := `bogus line
   8       0 sda 1 0 2 0 1 0 3 0 0 0 0
   8      16 sdb 1 0 bad 0 1 0 16 0 0 0 0
`
	allowed := map[string]struct{}{"sda": {}, "sdb": {}}
	totals, err := parseDiskStats(strings.NewReader(raw), allowed)
	if err != nil {
		t.Fatal(err)
	}
	if totals.readBytes != 2*diskSectorSize || totals.writeBytes != 3*diskSectorSize {
		t.Fatalf("parseDiskStats = (%d, %d), want (%d, %d)", totals.readBytes, totals.writeBytes, 2*diskSectorSize, 3*diskSectorSize)
	}
}

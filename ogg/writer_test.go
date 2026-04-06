package ogg

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestWriteBOS(t *testing.T) {
	var buf bytes.Buffer
	pw := NewPageWriter(&buf)

	data := []byte{1, 2, 3, 4, 5}
	if err := pw.WriteBOS(0, data); err != nil {
		t.Fatalf("WriteBOS failed: %v", err)
	}

	// Verify the page was written.
	page := buf.Bytes()
	if len(page) < 27 {
		t.Fatalf("page too short: %d", len(page))
	}

	// Check capture pattern.
	if string(page[:4]) != "OggS" {
		t.Errorf("bad capture pattern: %q", string(page[:4]))
	}

	// Check version.
	if page[4] != 0 {
		t.Errorf("bad version: %d", page[4])
	}

	// Check header_type (BOS flag = 0x02).
	if page[5] != 0x02 {
		t.Errorf("bad header_type: 0x%02x, expected 0x02", page[5])
	}

	// Check granule position (should be 0).
	granule := binary.LittleEndian.Uint64(page[6:14])
	if granule != 0 {
		t.Errorf("bad granule: %d", granule)
	}

	// Check serial number.
	serial := binary.LittleEndian.Uint32(page[14:18])
	if serial == 0 {
		t.Errorf("serial is zero")
	}

	// Check page sequence (should be 0 for first page).
	seq := binary.LittleEndian.Uint32(page[18:22])
	if seq != 0 {
		t.Errorf("bad sequence: %d", seq)
	}

	// Check CRC32 (should not be zero after calculation).
	crc := binary.LittleEndian.Uint32(page[22:26])
	if crc == 0 {
		t.Errorf("CRC32 is zero")
	}

	// Verify CRC32 is correct by recalculating.
	// Zero out the checksum field and recalculate.
	testPage := make([]byte, len(page))
	copy(testPage, page)
	binary.LittleEndian.PutUint32(testPage[22:26], 0)
	expectedCrc := crc32Ogg(testPage)
	if expectedCrc != crc {
		t.Errorf("CRC32 mismatch: got 0x%08x, expected 0x%08x", crc, expectedCrc)
	}
}

func TestWritePage(t *testing.T) {
	var buf bytes.Buffer
	pw := NewPageWriter(&buf)

	// Write BOS first.
	if err := pw.WriteBOS(0, []byte{1, 2, 3}); err != nil {
		t.Fatalf("WriteBOS failed: %v", err)
	}

	initialSize := buf.Len()

	// Write a regular page.
	if err := pw.WritePage(100, []byte{4, 5, 6, 7}); err != nil {
		t.Fatalf("WritePage failed: %v", err)
	}

	// Verify a second page was written.
	pages := buf.Bytes()
	if len(pages) <= initialSize {
		t.Fatalf("no new data written")
	}

	// Extract the second page.
	secondPageStart := initialSize
	secondPage := pages[secondPageStart:]

	// Check header_type (should be 0x00 for regular page).
	if secondPage[5] != 0x00 {
		t.Errorf("bad header_type: 0x%02x, expected 0x00", secondPage[5])
	}

	// Check granule position.
	granule := binary.LittleEndian.Uint64(secondPage[6:14])
	if granule != 100 {
		t.Errorf("bad granule: %d, expected 100", granule)
	}

	// Check page sequence.
	seq := binary.LittleEndian.Uint32(secondPage[18:22])
	if seq != 1 {
		t.Errorf("bad sequence: %d, expected 1", seq)
	}
}

func TestWriteEOS(t *testing.T) {
	var buf bytes.Buffer
	pw := NewPageWriter(&buf)

	// Write BOS and a regular page first.
	if err := pw.WriteBOS(0, []byte{1}); err != nil {
		t.Fatalf("WriteBOS failed: %v", err)
	}
	if err := pw.WritePage(100, []byte{2, 3}); err != nil {
		t.Fatalf("WritePage failed: %v", err)
	}

	initialSize := buf.Len()

	// Write EOS page.
	if err := pw.WriteEOS(200); err != nil {
		t.Fatalf("WriteEOS failed: %v", err)
	}

	pages := buf.Bytes()
	if len(pages) <= initialSize {
		t.Fatalf("no new data written")
	}

	// Extract the EOS page.
	eosPageStart := initialSize
	eosPage := pages[eosPageStart:]

	// Check header_type (EOS flag = 0x04).
	if eosPage[5] != 0x04 {
		t.Errorf("bad header_type: 0x%02x, expected 0x04", eosPage[5])
	}

	// Check granule position.
	granule := binary.LittleEndian.Uint64(eosPage[6:14])
	if granule != 200 {
		t.Errorf("bad granule: %d, expected 200", granule)
	}

	// EOS page should have no payload.
	// segment_count is at offset 26 from page start.
	segmentCount := eosPage[26]
	if segmentCount != 0 {
		t.Errorf("EOS page has segments: %d", segmentCount)
	}
}

func TestMultiplePages(t *testing.T) {
	var buf bytes.Buffer
	pw := NewPageWriter(&buf)

	// Write several pages.
	if err := pw.WriteBOS(0, []byte("header")); err != nil {
		t.Fatalf("WriteBOS failed: %v", err)
	}
	for i := 1; i <= 3; i++ {
		data := bytes.Repeat([]byte{byte(i)}, 100)
		if err := pw.WritePage(int64(i*100), data); err != nil {
			t.Fatalf("WritePage %d failed: %v", i, err)
		}
	}
	if err := pw.WriteEOS(400); err != nil {
		t.Fatalf("WriteEOS failed: %v", err)
	}

	pages := buf.Bytes()
	if len(pages) < 27*5 {
		t.Fatalf("pages too short")
	}

	// Count pages by searching for "OggS" patterns.
	count := 0
	for i := 0; i < len(pages)-3; i++ {
		if string(pages[i:i+4]) == "OggS" {
			count++
		}
	}

	if count != 5 {
		t.Errorf("expected 5 pages, found %d", count)
	}
}

func TestLargeData(t *testing.T) {
	var buf bytes.Buffer
	pw := NewPageWriter(&buf)

	// Create large data that requires multiple segments.
	largeData := bytes.Repeat([]byte{0xAA}, 1000)

	if err := pw.WriteBOS(0, largeData); err != nil {
		t.Fatalf("WriteBOS with large data failed: %v", err)
	}

	page := buf.Bytes()
	if len(page) < 27 {
		t.Fatalf("page too short")
	}

	// Check that multiple segments were created.
	segmentCount := int(page[26])
	if segmentCount <= 1 {
		t.Errorf("expected multiple segments, got %d", segmentCount)
	}

	// Verify segment table sums to at least 1000.
	payloadSize := 0
	for i := 0; i < segmentCount; i++ {
		payloadSize += int(page[27+i])
	}
	if payloadSize != 1000 {
		t.Errorf("payload size mismatch: got %d, expected 1000", payloadSize)
	}
}

func BenchmarkWritePage(b *testing.B) {
	var buf bytes.Buffer
	pw := NewPageWriter(&buf)
	data := bytes.Repeat([]byte{0x42}, 1000)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		_ = pw.WritePage(int64(i), data)
	}
}

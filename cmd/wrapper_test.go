package cmd

import (
	"reflect"
	"testing"
)

func TestSegmentMessage(t *testing.T) {

	testGoodStrings := []string{
		"test 1 2 3 4",
		"<@1231231URAS>  test 1 2  3 4 ",
		" test 1 2 3 4",
		"1 2 3 4 ",
		"<@1231231UARAS>  test 1 2 3 4 ",
		" 	<@1231231UARAS>  test 1 2 3   4 ",
	}

	expect := []string{"1", "2", "3", "4"}

	for _, v := range testGoodStrings {
		args := segmentMessage("test", v)

		if !reflect.DeepEqual(args, expect) {
			t.Fatalf("expected: %v got %v", expect, v)
		}
	}

	badStrings := []string{
		"test",
		"<@123123> test ",
	}

	expect = []string{}
	for _, v := range badStrings {
		args := segmentMessage("test", v)
		if !reflect.DeepEqual(args, expect) {
			t.Fatalf("expected: %v got %v", expect, v)
		}
	}
}

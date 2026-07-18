package provider

import (
	"reflect"
	"testing"
)

func TestAWSAccountModelContainsOnlyPublicAWSIdentity(t *testing.T) {
	typeOfModel := reflect.TypeOf(awsAccountModel{})
	attributes := map[string]bool{}
	for index := 0; index < typeOfModel.NumField(); index++ {
		attributes[typeOfModel.Field(index).Tag.Get("tfsdk")] = true
	}
	for _, expected := range []string{"account_id", "iam_role", "regions", "sts_region", "readonly", "external_id", "status"} {
		if !attributes[expected] {
			t.Fatalf("model missing public attribute %q", expected)
		}
	}
	for _, forbidden := range []string{"id", "provider_id"} {
		if attributes[forbidden] {
			t.Fatalf("model must not contain internal attribute %q", forbidden)
		}
	}
}

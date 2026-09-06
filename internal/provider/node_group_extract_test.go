package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestDesiredWorkers(t *testing.T) {
	tests := []struct {
		name string
		sp   *ngScalePolicyModel
		want int
	}{
		{"nil", nil, 0},
		{"fixed", &ngScalePolicyModel{FixedScale: &ngFixedScaleModel{Size: types.Int64Value(3)}}, 3},
		{"fixed zero", &ngScalePolicyModel{FixedScale: &ngFixedScaleModel{Size: types.Int64Value(0)}}, 0},
		{"auto initial", &ngScalePolicyModel{AutoScale: &ngAutoScaleModel{
			InitialSize: types.Int64Value(2), MinSize: types.Int64Value(1), MaxSize: types.Int64Value(5),
		}}, 2},
		{"auto min only", &ngScalePolicyModel{AutoScale: &ngAutoScaleModel{
			InitialSize: types.Int64Null(), MinSize: types.Int64Value(4), MaxSize: types.Int64Value(9),
		}}, 4},
	}
	for _, tc := range tests {
		if got := desiredWorkers(tc.sp); got != tc.want {
			t.Errorf("%s: desiredWorkers = %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestDesiredRAMGiB(t *testing.T) {
	tests := []struct {
		name string
		it   *ngInstanceTemplateModel
		want int
	}{
		{"nil", nil, 0},
		{"no resources", &ngInstanceTemplateModel{}, 0},
		{"null memory", &ngInstanceTemplateModel{Resources: &ngResourcesModel{Memory: types.Float64Null()}}, 0},
		{"16 GiB", &ngInstanceTemplateModel{Resources: &ngResourcesModel{Memory: types.Float64Value(16)}}, 16},
		{"7.6 GiB rounds", &ngInstanceTemplateModel{Resources: &ngResourcesModel{Memory: types.Float64Value(7.6)}}, 8},
	}
	for _, tc := range tests {
		if got := desiredRAMGiB(tc.it); got != tc.want {
			t.Errorf("%s: desiredRAMGiB = %d, want %d", tc.name, got, tc.want)
		}
	}
}

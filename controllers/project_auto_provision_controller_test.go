package controllers

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v1 "github.com/dinoallo/labring-sigs-harbor/api/v1"
)

func newAutoProvisionReconciler(ownerLabelKey string, objs ...runtime.Object) *ProjectAutoProvisionReconciler {
	scheme := runtime.NewScheme()
	_ = v1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(objs...).
		Build()

	return &ProjectAutoProvisionReconciler{
		Client:        fakeClient,
		Scheme:        scheme,
		OwnerLabelKey: ownerLabelKey,
	}
}

func ns(name string, labels map[string]string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
			UID:    types.UID(name + "-uid"),
		},
	}
}

func hp(name, owner, namespace string) *v1.HarborProject {
	return &v1.HarborProject{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				AutoProvisionLabel:      "true",
				SourceNamespaceLabel:    namespace,
				SourceNamespaceUIDLabel: namespace + "-uid",
			},
		},
		Spec: v1.HarborProjectSpec{
			Owner:         owner,
			ProjectName:   namespace,
			NamespaceRefs: []string{namespace},
			StorageLimit:  5 * 1024 * 1024 * 1024,
			Public:        false,
			AutoScan:      false,
			RobotPermissions: []v1.RobotPermission{
				{Action: "push"},
				{Action: "pull"},
			},
		},
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestAutoProvision_Namespace_WithLabel_CreatesHarborProject(t *testing.T) {
	ownerLabelKey := "user.sealos.io/owner"
	nsObj := ns("ns-1", map[string]string{ownerLabelKey: "user-abc"})

	r := newAutoProvisionReconciler(ownerLabelKey, nsObj)
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "ns-1"}}

	result, err := r.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Requeue {
		t.Fatal("expected no requeue")
	}

	// Verify HarborProject was created
	hpObj := &v1.HarborProject{}
	if err := r.Get(context.Background(), types.NamespacedName{Name: "hp-ns-1"}, hpObj); err != nil {
		t.Fatalf("expected HarborProject to be created: %v", err)
	}
	if hpObj.Spec.Owner != "user-abc" {
		t.Errorf("expected owner 'user-abc', got %q", hpObj.Spec.Owner)
	}
	if len(hpObj.Spec.NamespaceRefs) != 1 || hpObj.Spec.NamespaceRefs[0] != "ns-1" {
		t.Errorf("expected namespaceRefs [ns-1], got %v", hpObj.Spec.NamespaceRefs)
	}
	if hpObj.Spec.StorageLimit != 5*1024*1024*1024 {
		t.Errorf("expected storageLimit 5GB, got %d", hpObj.Spec.StorageLimit)
	}
	if hpObj.Spec.Public {
		t.Error("expected public false")
	}
	if hpObj.Spec.AutoScan {
		t.Error("expected autoScan false")
	}
	if hpObj.Labels[AutoProvisionLabel] != "true" {
		t.Errorf("expected label %s=true", AutoProvisionLabel)
	}
	if hpObj.Labels[SourceNamespaceLabel] != "ns-1" {
		t.Errorf("expected label %s=ns-1", SourceNamespaceLabel)
	}
	if hpObj.Labels[SourceNamespaceUIDLabel] == "" {
		t.Errorf("expected label %s to be set", SourceNamespaceUIDLabel)
	}
}

func TestAutoProvision_Namespace_WithoutLabel_DoesNothing(t *testing.T) {
	ownerLabelKey := "user.sealos.io/owner"
	nsObj := ns("ns-2", map[string]string{"some-other-label": "val"})

	r := newAutoProvisionReconciler(ownerLabelKey, nsObj)
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "ns-2"}}

	result, err := r.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Requeue {
		t.Fatal("expected no requeue")
	}

	// Verify no HarborProject was created
	hpObj := &v1.HarborProject{}
	err = r.Get(context.Background(), types.NamespacedName{Name: "hp-ns-2"}, hpObj)
	if err == nil {
		t.Fatal("expected HarborProject to NOT exist")
	}
}

func TestAutoProvision_Namespace_WithEmptyLabel_DoesNothing(t *testing.T) {
	ownerLabelKey := "user.sealos.io/owner"
	nsObj := ns("ns-3", map[string]string{ownerLabelKey: ""})

	r := newAutoProvisionReconciler(ownerLabelKey, nsObj)
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "ns-3"}}

	result, err := r.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Requeue {
		t.Fatal("expected no requeue")
	}

	hpObj := &v1.HarborProject{}
	err = r.Get(context.Background(), types.NamespacedName{Name: "hp-ns-3"}, hpObj)
	if err == nil {
		t.Fatal("expected HarborProject to NOT exist when label value is empty")
	}
}

func TestAutoProvision_ExistingHP_SpecMatches_NoUpdate(t *testing.T) {
	ownerLabelKey := "user.sealos.io/owner"
	nsObj := ns("ns-4", map[string]string{ownerLabelKey: "user-abc"})
	existingHP := hp("hp-ns-4", "user-abc", "ns-4")

	r := newAutoProvisionReconciler(ownerLabelKey, nsObj, existingHP)

	// Read the existing HP's resource version before reconcile
	before := &v1.HarborProject{}
	if err := r.Get(context.Background(), types.NamespacedName{Name: "hp-ns-4"}, before); err != nil {
		t.Fatalf("failed to get existing HP: %v", err)
	}
	origRV := before.ResourceVersion

	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "ns-4"}}
	result, err := r.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Requeue {
		t.Fatal("expected no requeue")
	}

	// Verify ResourceVersion did not change (no update was written)
	after := &v1.HarborProject{}
	if err := r.Get(context.Background(), types.NamespacedName{Name: "hp-ns-4"}, after); err != nil {
		t.Fatalf("failed to get HP after reconcile: %v", err)
	}
	if after.ResourceVersion != origRV {
		t.Errorf("expected no update, but ResourceVersion changed from %s to %s", origRV, after.ResourceVersion)
	}
}

func TestAutoProvision_ExistingHP_SpecDiffers_Updates(t *testing.T) {
	ownerLabelKey := "user.sealos.io/owner"
	nsObj := ns("ns-5", map[string]string{ownerLabelKey: "user-abc"})

	// Create an existing HP with different owner value
	existingHP := hp("hp-ns-5", "user-old", "ns-5")

	r := newAutoProvisionReconciler(ownerLabelKey, nsObj, existingHP)

	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "ns-5"}}
	result, err := r.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Requeue {
		t.Fatal("expected no requeue")
	}

	// Verify the HP was updated
	after := &v1.HarborProject{}
	if err := r.Get(context.Background(), types.NamespacedName{Name: "hp-ns-5"}, after); err != nil {
		t.Fatalf("failed to get HP: %v", err)
	}
	if after.Spec.Owner != "user-abc" {
		t.Errorf("expected owner to be updated to 'user-abc', got %q", after.Spec.Owner)
	}
}

func TestAutoProvision_LabelValueChanged_UpdatesOwner(t *testing.T) {
	ownerLabelKey := "user.sealos.io/owner"

	// Start with owner label "user-old", then update it to "user-new"
	nsObj := ns("ns-6", map[string]string{ownerLabelKey: "user-new"})
	existingHP := hp("hp-ns-6", "user-old", "ns-6")

	r := newAutoProvisionReconciler(ownerLabelKey, nsObj, existingHP)

	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "ns-6"}}
	result, err := r.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Requeue {
		t.Fatal("expected no requeue")
	}

	after := &v1.HarborProject{}
	if err := r.Get(context.Background(), types.NamespacedName{Name: "hp-ns-6"}, after); err != nil {
		t.Fatalf("failed to get HP: %v", err)
	}
	if after.Spec.Owner != "user-new" {
		t.Errorf("expected owner to be updated to 'user-new', got %q", after.Spec.Owner)
	}
}

func TestAutoProvision_LabelRemoved_DoesNothing(t *testing.T) {
	ownerLabelKey := "user.sealos.io/owner"

	// NS no longer has the label, but HP still exists
	nsObj := ns("ns-7", map[string]string{"other": "val"})
	existingHP := hp("hp-ns-7", "user-old", "ns-7")

	r := newAutoProvisionReconciler(ownerLabelKey, nsObj, existingHP)

	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "ns-7"}}
	result, err := r.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Requeue {
		t.Fatal("expected no requeue")
	}

	// HP should still exist and be unchanged
	after := &v1.HarborProject{}
	if err := r.Get(context.Background(), types.NamespacedName{Name: "hp-ns-7"}, after); err != nil {
		t.Fatalf("expected HP to still exist: %v", err)
	}
	if after.Spec.Owner != "user-old" {
		t.Errorf("expected owner still 'user-old', got %q", after.Spec.Owner)
	}
}

func TestAutoProvision_NamespaceNotFound_NoError(t *testing.T) {
	ownerLabelKey := "user.sealos.io/owner"
	r := newAutoProvisionReconciler(ownerLabelKey)

	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "nonexistent"}}
	result, err := r.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("expected no error for missing namespace, got: %v", err)
	}
	if result.Requeue {
		t.Fatal("expected no requeue for missing namespace")
	}
}

func TestAutoProvision_AdoptsExistingHP_WithWrongSpec(t *testing.T) {
	ownerLabelKey := "user.sealos.io/owner"
	nsObj := ns("ns-8", map[string]string{ownerLabelKey: "user-abc"})

	// Manually-created HP with different spec (different owner, different storageLimit)
	existingHP := &v1.HarborProject{
		ObjectMeta: metav1.ObjectMeta{
			Name: "hp-ns-8",
			Labels: map[string]string{
				"some-other-label": "val",
			},
		},
		Spec: v1.HarborProjectSpec{
			Owner:         "user-manual",
			NamespaceRefs: []string{"other-ns"},
			StorageLimit:  -1,
			Public:        true,
			AutoScan:      true,
			RobotPermissions: []v1.RobotPermission{
				{Action: "push"},
			},
		},
	}

	r := newAutoProvisionReconciler(ownerLabelKey, nsObj, existingHP)

	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "ns-8"}}
	result, err := r.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Requeue {
		t.Fatal("expected no requeue")
	}

	// Verify the HP was adopted (spec updated to match desired)
	after := &v1.HarborProject{}
	if err := r.Get(context.Background(), types.NamespacedName{Name: "hp-ns-8"}, after); err != nil {
		t.Fatalf("failed to get HP: %v", err)
	}
	if after.Spec.Owner != "user-abc" {
		t.Errorf("expected owner 'user-abc', got %q", after.Spec.Owner)
	}
	if len(after.Spec.NamespaceRefs) != 1 || after.Spec.NamespaceRefs[0] != "ns-8" {
		t.Errorf("expected namespaceRefs [ns-8], got %v", after.Spec.NamespaceRefs)
	}
	if after.Spec.StorageLimit != 5*1024*1024*1024 {
		t.Errorf("expected storageLimit 5GB, got %d", after.Spec.StorageLimit)
	}
	if after.Spec.Public {
		t.Error("expected public false")
	}
	// Verify auto-provision labels are added
	if after.Labels[AutoProvisionLabel] != "true" {
		t.Errorf("expected auto-provision label to be added")
	}
	if after.Labels[SourceNamespaceLabel] != "ns-8" {
		t.Errorf("expected label %s=ns-8, got %q", SourceNamespaceLabel, after.Labels[SourceNamespaceLabel])
	}
	if after.Labels[SourceNamespaceUIDLabel] == "" {
		t.Errorf("expected label %s to be set", SourceNamespaceUIDLabel)
	}
}

func TestAutoProvision_ExistingHP_MissingLabels_TriggersUpdate(t *testing.T) {
	ownerLabelKey := "user.sealos.io/owner"
	nsObj := ns("ns-9", map[string]string{ownerLabelKey: "user-abc"})

	// Existing CR that has matching spec but is missing auto-provision labels
	existingHP := &v1.HarborProject{
		ObjectMeta: metav1.ObjectMeta{
			Name: "hp-ns-9",
			Labels: map[string]string{
				"some-other-label": "val",
			},
		},
		Spec: v1.HarborProjectSpec{
			Owner:         "user-abc",
			NamespaceRefs: []string{"ns-9"},
			StorageLimit:  5 * 1024 * 1024 * 1024,
			Public:        false,
			AutoScan:      false,
			RobotPermissions: []v1.RobotPermission{
				{Action: "push"},
				{Action: "pull"},
			},
		},
	}

	r := newAutoProvisionReconciler(ownerLabelKey, nsObj, existingHP)
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "ns-9"}}

	result, err := r.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Requeue {
		t.Fatal("expected no requeue")
	}

	// Verify the HP was updated with labels
	after := &v1.HarborProject{}
	if err := r.Get(context.Background(), types.NamespacedName{Name: "hp-ns-9"}, after); err != nil {
		t.Fatalf("failed to get HP: %v", err)
	}
	if after.Labels[AutoProvisionLabel] != "true" {
		t.Errorf("expected label %s=true, got %q", AutoProvisionLabel, after.Labels[AutoProvisionLabel])
	}
	if after.Labels[SourceNamespaceLabel] != "ns-9" {
		t.Errorf("expected label %s=ns-9, got %q", SourceNamespaceLabel, after.Labels[SourceNamespaceLabel])
	}
	if after.Labels[SourceNamespaceUIDLabel] == "" {
		t.Errorf("expected label %s to be set", SourceNamespaceUIDLabel)
	}
}

package k8s

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

func TestEventFieldSelector(t *testing.T) {
	tests := []struct {
		name                string
		namespace, obj, uid string
		want                string
	}{
		{
			name:      "namespaced with uid",
			namespace: "default",
			obj:       "nginx-1",
			uid:       "abc-123",
			want:      "involvedObject.name=nginx-1,involvedObject.namespace=default,involvedObject.uid=abc-123",
		},
		{
			name: "cluster scoped omits namespace",
			obj:  "node-1",
			uid:  "abc-123",
			want: "involvedObject.name=node-1,involvedObject.uid=abc-123",
		},
		{
			name:      "unknown uid omits uid",
			namespace: "kube-system",
			obj:       "coredns",
			want:      "involvedObject.name=coredns,involvedObject.namespace=kube-system",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := eventFieldSelector(tt.namespace, tt.obj, tt.uid)
			if got != tt.want {
				t.Fatalf("eventFieldSelector() = %q, want %q", got, tt.want)
			}
		})
	}
}

// mkEvent builds an event whose most recent occurrence is agoMinutes ago.
func mkEvent(name, ns, uid, evType, reason, msg string, count int32, agoMinutes int) corev1.Event {
	return corev1.Event{
		ObjectMeta: metav1.ObjectMeta{Name: name + ".1", Namespace: ns},
		InvolvedObject: corev1.ObjectReference{
			Kind: "Pod", Name: name, Namespace: ns, UID: types.UID(uid),
		},
		Type:          evType,
		Reason:        reason,
		Message:       msg,
		Count:         count,
		LastTimestamp: metav1.NewTime(time.Now().Add(-time.Duration(agoMinutes) * time.Minute)),
	}
}

func TestObjectEventsFromFiltersSortsAndConverts(t *testing.T) {
	items := []corev1.Event{
		mkEvent("nginx", "default", "uid-1", "Normal", "Scheduled", "Successfully assigned", 1, 10),
		mkEvent("nginx", "default", "uid-1", "Warning", "BackOff", "Back-off restarting\nfailed container", 5, 2),
		// Same name, different object: a recreated pod must not inherit these.
		mkEvent("nginx", "default", "uid-old", "Warning", "Stale", "from a previous pod", 1, 1),
		mkEvent("other", "default", "uid-2", "Normal", "Pulled", "pulled image", 1, 1),
		mkEvent("nginx", "kube-system", "uid-3", "Normal", "Elsewhere", "other namespace", 1, 1),
	}

	got := objectEventsFrom(items, "default", "nginx", "uid-1", MaxObjectEvents)
	if len(got) != 2 {
		t.Fatalf("objectEventsFrom() returned %d events, want 2: %+v", len(got), got)
	}
	// Newest first.
	if got[0].Reason != "BackOff" || got[1].Reason != "Scheduled" {
		t.Fatalf("events not newest first: %+v", got)
	}
	if got[0].Type != "Warning" || got[0].Count != 5 {
		t.Fatalf("event type/count = %q/%d, want Warning/5", got[0].Type, got[0].Count)
	}
	if got[0].Message != "Back-off restarting failed container" {
		t.Fatalf("message newline not flattened: %q", got[0].Message)
	}
	if got[0].Age == "" || got[0].Age == "-" {
		t.Fatalf("event age = %q, want a compact age", got[0].Age)
	}
}

func TestObjectEventsFromClampsCountAndLimit(t *testing.T) {
	items := []corev1.Event{
		mkEvent("nginx", "default", "uid-1", "Normal", "First", "one", 0, 3),
		mkEvent("nginx", "default", "uid-1", "Normal", "Second", "two", 0, 2),
		mkEvent("nginx", "default", "uid-1", "Normal", "Third", "three", 0, 1),
	}

	got := objectEventsFrom(items, "default", "nginx", "uid-1", 2)
	if len(got) != 2 {
		t.Fatalf("limit not applied: got %d events, want 2", len(got))
	}
	if got[0].Reason != "Third" {
		t.Fatalf("limit dropped the newest events: %+v", got)
	}
	for _, e := range got {
		if e.Count != 1 {
			t.Fatalf("zero Count = %d, want clamped to 1", e.Count)
		}
	}
}

func TestObjectEventsFromMatchesWhenUIDUnrecorded(t *testing.T) {
	// Older events may carry no involvedObject uid; they still belong to the object.
	items := []corev1.Event{mkEvent("nginx", "default", "", "Warning", "Unhealthy", "probe failed", 1, 1)}

	got := objectEventsFrom(items, "default", "nginx", "uid-1", MaxObjectEvents)
	if len(got) != 1 {
		t.Fatalf("event with empty uid was dropped: %+v", got)
	}
}

func TestObjectEventsFiltersServerSideAndLocally(t *testing.T) {
	// The fake clientset ignores field selectors, so this exercises the local match.
	mine := mkEvent("nginx", "default", "uid-1", "Warning", "BackOff", "back-off", 3, 1)
	theirs := mkEvent("redis", "default", "uid-2", "Normal", "Pulled", "pulled", 1, 1)
	c := &Client{clientset: fake.NewSimpleClientset(&mine, &theirs)}

	got, err := c.ObjectEvents(context.Background(), ResourceInfo{Resource: "pods", Kind: "Pod"}, "default", "nginx", "uid-1")
	if err != nil {
		t.Fatalf("ObjectEvents() error = %v", err)
	}
	if len(got) != 1 || got[0].Reason != "BackOff" {
		t.Fatalf("ObjectEvents() = %+v, want only the BackOff event", got)
	}
}

func TestObjectEventsWithoutNameReturnsNothing(t *testing.T) {
	c := &Client{clientset: fake.NewSimpleClientset()}
	got, err := c.ObjectEvents(context.Background(), ResourceInfo{Resource: "pods"}, "default", "", "")
	if err != nil || got != nil {
		t.Fatalf("ObjectEvents() = %+v, %v; want nil, nil", got, err)
	}
}

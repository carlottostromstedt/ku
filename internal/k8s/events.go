package k8s

import (
	"context"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// MaxObjectEvents caps how many events are kept for one object. Events are the
// last section of the config summary, so a flapping object cannot be allowed to
// bury everything above it.
const MaxObjectEvents = 25

// eventListLimit is how many events are pulled from the server before filtering.
// The API cannot always narrow by involvedObject, so we over-fetch and match
// locally.
const eventListLimit = 200

// ObjectEvent is a single event recorded against one object.
type ObjectEvent struct {
	Age     string // compact age of the most recent occurrence, e.g. "2m"
	Type    string // Normal or Warning
	Reason  string
	Message string
	Count   int32 // server-side occurrence count, at least 1
}

// ObjectEvents returns the events recorded against one object, newest first.
// namespace is empty for cluster-scoped resources, which lists across all
// namespaces the way kubectl does for nodes. uid may be empty; the match then
// falls back to name and namespace alone.
func (c *Client) ObjectEvents(ctx context.Context, res ResourceInfo, namespace, name, uid string) ([]ObjectEvent, error) {
	if name == "" {
		return nil, nil
	}
	// Core v1 also serves events written through events.k8s.io/v1, so one read
	// covers both APIs. This is why kubectl still describes from core v1.
	list, err := c.clientset.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{
		FieldSelector: eventFieldSelector(namespace, name, uid),
		Limit:         eventListLimit,
	})
	if err != nil {
		return nil, err
	}
	return objectEventsFrom(list.Items, namespace, name, uid, MaxObjectEvents), nil
}

// eventFieldSelector narrows the list server-side. Empty parts are omitted:
// cluster-scoped objects have no namespace, and the uid is unknown if the caller
// only has a name.
func eventFieldSelector(namespace, name, uid string) string {
	parts := []string{"involvedObject.name=" + name}
	if namespace != "" {
		parts = append(parts, "involvedObject.namespace="+namespace)
	}
	if uid != "" {
		parts = append(parts, "involvedObject.uid="+uid)
	}
	return strings.Join(parts, ",")
}

// objectEventsFrom filters, sorts newest first, and converts raw events. The
// field selector is re-applied locally: not every API server narrows events
// server-side, and a stale event for a recreated object with the same name would
// otherwise be attributed to the new one.
func objectEventsFrom(items []corev1.Event, namespace, name, uid string, limit int) []ObjectEvent {
	matched := make([]*corev1.Event, 0, len(items))
	for i := range items {
		if eventMatchesObject(&items[i], namespace, name, uid) {
			matched = append(matched, &items[i])
		}
	}
	sort.SliceStable(matched, func(i, j int) bool {
		return eventTime(matched[i]).After(eventTime(matched[j]))
	})
	if limit > 0 && len(matched) > limit {
		matched = matched[:limit]
	}

	out := make([]ObjectEvent, 0, len(matched))
	for _, e := range matched {
		count := e.Count
		if count < 1 {
			count = 1
		}
		out = append(out, ObjectEvent{
			Age:     ageString(eventTime(e)),
			Type:    e.Type,
			Reason:  e.Reason,
			Message: flattenMessage(e.Message),
			Count:   count,
		})
	}
	return out
}

func eventMatchesObject(e *corev1.Event, namespace, name, uid string) bool {
	if e.InvolvedObject.Name != name {
		return false
	}
	if namespace != "" && e.InvolvedObject.Namespace != namespace {
		return false
	}
	// Only compare uids when both sides have one. An event may predate the uid
	// being recorded, and the caller may not know it.
	if uid != "" && e.InvolvedObject.UID != "" && string(e.InvolvedObject.UID) != uid {
		return false
	}
	return true
}

// flattenMessage collapses a multi-line event message to one line. Messages are
// server-authored free text and the pager splits content on newlines, so an
// embedded newline would silently split one event across rows.
func flattenMessage(s string) string {
	if !strings.ContainsAny(s, "\r\n") {
		return strings.TrimSpace(s)
	}
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	return strings.TrimSpace(s)
}

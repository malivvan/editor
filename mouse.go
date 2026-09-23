package editor

// MouseAction is the logical action a mouse event represents.
//
// View derives these actions from raw tcell mouse events (see
// View.HandleMouse); a host that keeps its own input layer may instead call
// View.HandleMouseAction directly with the action it already knows.
type MouseAction int16

// Mouse actions recognized by the editor.
//
// There are no click or double click actions: the editor tells consecutive
// clicks apart itself, from the time between the down and the up events (see
// DOUBLE_CLICK_INTERVAL).
const (
	MouseMove MouseAction = iota
	MouseLeftDown
	MouseLeftUp
	MouseMiddleDown
	MouseMiddleUp
	MouseRightDown
	MouseRightUp
	MouseScrollUp
	MouseScrollDown
	MouseScrollLeft
	MouseScrollRight
)

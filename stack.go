package editor

// Stack is a simple implementation of a LIFO stack for text events
type Stack struct {
	Top  *Element
	Size int
}

// An Element which is stored in the Stack
type Element struct {
	Value *TextEvent
	Next  *Element
}

// Len returns the stack's length
func (s *Stack) Len() int {
	return s.Size
}

// Push a new element onto the stack
func (s *Stack) Push(value *TextEvent) {
	s.Top = &Element{value, s.Top}
	s.Size++
}

// Pop removes the top element from the stack and returns its value
// If the stack is empty, return nil
func (s *Stack) Pop() (value *TextEvent) {
	if s.Size > 0 {
		value, s.Top = s.Top.Value, s.Top.Next
		s.Size--
		return
	}
	return nil
}

// Peek returns the top element of the stack without removing it
func (s *Stack) Peek() *TextEvent {
	if s.Size > 0 {
		return s.Top.Value
	}
	return nil
}

// Trim removes the oldest elements so that the stack contains at most max
// elements.  It walks from the top to the (max)th element and cuts the tail.
func (s *Stack) Trim(max int) {
	if max <= 0 || s.Size <= max {
		return
	}
	cur := s.Top
	for i := 1; i < max; i++ {
		cur = cur.Next
	}
	cur.Next = nil
	s.Size = max
}

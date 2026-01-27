package git

import "context"

// MockCommander is a mock implementation of Commander for testing
type MockCommander struct {
	// RunFunc is called when Run is invoked
	RunFunc func(ctx context.Context, dir string, args ...string) error

	// OutputFunc is called when Output is invoked
	OutputFunc func(ctx context.Context, dir string, args ...string) (string, error)

	// Calls records all method calls for verification
	Calls []MockCommanderCall
}

// MockCommanderCall records a method call
type MockCommanderCall struct {
	Method string
	Dir    string
	Args   []string
}

// NewMockCommander creates a new mock commander
func NewMockCommander() *MockCommander {
	return &MockCommander{
		Calls: make([]MockCommanderCall, 0),
	}
}

// Run implements Commander.Run
func (m *MockCommander) Run(ctx context.Context, dir string, args ...string) error {
	m.Calls = append(m.Calls, MockCommanderCall{Method: "Run", Dir: dir, Args: args})
	if m.RunFunc != nil {
		return m.RunFunc(ctx, dir, args...)
	}
	return nil
}

// Output implements Commander.Output
func (m *MockCommander) Output(ctx context.Context, dir string, args ...string) (string, error) {
	m.Calls = append(m.Calls, MockCommanderCall{Method: "Output", Dir: dir, Args: args})
	if m.OutputFunc != nil {
		return m.OutputFunc(ctx, dir, args...)
	}
	return "", nil
}

// Reset clears all recorded calls
func (m *MockCommander) Reset() {
	m.Calls = make([]MockCommanderCall, 0)
}

// CallCount returns the number of times a method was called
func (m *MockCommander) CallCount(method string) int {
	count := 0
	for _, call := range m.Calls {
		if call.Method == method {
			count++
		}
	}
	return count
}

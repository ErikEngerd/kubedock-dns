package support

import (
	"github.com/stretchr/testify/suite"
	"testing"
)

type LinkedMapTestSuite struct {
	suite.Suite
}

func (s *LinkedMapTestSuite) SetupSuite() {
	checkStatus = true
}

func (s *LinkedMapTestSuite) TearDownSuite() {
	checkStatus = false
}

func (s *LinkedMapTestSuite) SetupTest() {
}

func (s *LinkedMapTestSuite) TearDownTest() {
}

func TestLinkedMapSuite(t *testing.T) {
	suite.Run(t, &LinkedMapTestSuite{})
}

func (s *LinkedMapTestSuite) contentCheck(m *LinkedMap[string, int],
	keys []string, values []int) {

	s.True(len(keys) == len(values), "input error expected keys and values differ in length")

	// keys
	i := 0
	for key := range m.RangeKeys() {
		s.True(i < len(keys), "Too many elements in map")
		s.Equal(keys[i], key)
		i++
	}
	s.Equal(len(keys), i)

	// values
	i = 0
	for value := range m.RangeValues() {
		s.True(i < len(values), "Too many elements in map")
		s.Equal(values[i], value)
		i++
	}
	s.Equal(len(values), i)

	// Entries
	i = 0
	for entry := range m.RangeEntries() {
		s.True(i < len(values), "Too many elements in map")
		s.Equal(keys[i], entry.Key)
		s.Equal(values[i], entry.Value)
		i++
	}
	s.Equal(len(values), i)

	// Get and Contains
	for i, key := range keys {
		v, ok := m.Get(key)
		s.True(ok)
		s.Equal(values[i], v)
		s.True(m.Contains(key))
	}
}

func (s *LinkedMapTestSuite) Test_emptymap() {
	m := NewLinkedMap[string, int]()
	s.contentCheck(m, []string{}, []int{})
}

func (s *LinkedMapTestSuite) Test_elementAddRemove() {
	m := NewLinkedMap[string, int]()
	m.Put("a", 1)
	s.contentCheck(m, []string{"a"}, []int{1})

	s.False(m.Delete("b"))
	s.contentCheck(m, []string{"a"}, []int{1})

	s.True(m.Delete("a"))
	s.contentCheck(m, []string{}, []int{})
}

func (s *LinkedMapTestSuite) Test_GetContainsForElementsNotInMap() {
	m := s.createSimpleMap()

	s.False(m.Contains("d"))
	val, ok := m.Get("d")
	s.False(ok)
	s.Equal(0, val)
}

func (s *LinkedMapTestSuite) Test_elementRemoveBeginning() {
	m := s.createSimpleMap()

	s.True(m.Delete("a"))
	s.contentCheck(m, []string{"b", "c"}, []int{2, 3})
}

func (s *LinkedMapTestSuite) Test_elementRemoveMiddle() {
	m := s.createSimpleMap()

	s.True(m.Delete("b"))
	s.contentCheck(m, []string{"a", "c"}, []int{1, 3})
}

func (s *LinkedMapTestSuite) Test_elementRemoveEnd() {
	m := s.createSimpleMap()

	s.True(m.Delete("c"))
	s.contentCheck(m, []string{"a", "b"}, []int{1, 2})
}

func (s *LinkedMapTestSuite) Test_addSameElementAgain() {
	m := s.createSimpleMap()

	m.Put("b", 4)
	s.contentCheck(m, []string{"a", "c", "b"}, []int{1, 3, 4})
}

func (s *LinkedMapTestSuite) createSimpleMap() *LinkedMap[string, int] {
	m := NewLinkedMap[string, int]()
	m.Put("a", 1)
	m.Put("b", 2)
	m.Put("c", 3)
	s.contentCheck(m, []string{"a", "b", "c"}, []int{1, 2, 3})
	return m
}

func (s *LinkedMapTestSuite) Test_manyElements() {
	m := NewLinkedMap[string, int]()
	chars := "0123456789"
	for i := 0; i < 10000; i++ {
		m.Put(chars[i%10:i%10+1], i)
	}
	s.contentCheck(m,
		[]string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9"},
		[]int{9990, 9991, 9992, 9993, 9994, 9995, 9996, 9997, 9998, 9999})
}

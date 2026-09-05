package analyzer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAuditPatterns(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"go.mod": `module example.com/audit

go 1.25.0
require (
 github.com/stretchr/testify v0.0.0
 pgregory.net/rapid v0.0.0
)
replace github.com/stretchr/testify => ./testify
replace pgregory.net/rapid => ./rapid
`,
		"prod.go": `package audit
func Add(a,b int)int{return a+b}
type Record struct { N int }
type Getter interface { Get() int }
func Write(r *Record){r.N=2}
func Ready() <-chan bool { return make(chan bool) }
`,
		"support/support.go": `package support
import "example.com/audit"
func Literal()int{return 2}
func Production()int{return audit.Add(1,1)}
`,
		"support/support_test.go": `package support
import "testing"
func TestSupportOnly(t *testing.T){t.Fatal("fixture")}
`,
		"testify/go.mod": "module github.com/stretchr/testify\n\ngo 1.25.0\n",
		"testify/assert/assert.go": `package assert
import "time"
type TestingT interface{Errorf(string,...any)}
type Assertions struct{}
type CollectT struct{}
func (*CollectT) Errorf(string,...any){}
func New(TestingT)*Assertions{return &Assertions{}}
func Equal(TestingT,any,any,...any)bool{return true}
func True(TestingT,bool,...any)bool{return true}
func NotPanics(TestingT,func(),...any)bool{return true}
func (a *Assertions) Equal(any,any,...any)bool{return true}
func Eventually(TestingT,func()bool,time.Duration,time.Duration,...any)bool{return true}
func EventuallyWithT(TestingT,func(*CollectT),time.Duration,time.Duration,...any)bool{return true}
`,
		"testify/suite/suite.go": `package suite
import (
 "testing"
 "github.com/stretchr/testify/assert"
)
type Suite struct{ *assert.Assertions }
func Run(*testing.T,any){}
func (*Suite) T()*testing.T{return nil}
func (*Suite) Require()*assert.Assertions{return nil}
func (*Suite) Run(string,func())bool{return true}
`,
		"rapid/go.mod": "module pgregory.net/rapid\n\ngo 1.25.0\n",
		"rapid/rapid.go": `package rapid
import "testing"
type T struct{*testing.T}
func Check(*testing.T,func(*T)){}
func (*T) Repeat(any){}
func StateMachineActions(any)any{return nil}
`,
		"audit_test.go": `package audit_test
import (
 "testing"
 "time"
 "example.com/audit"
 "example.com/audit/support"
 check "github.com/stretchr/testify/assert"
 "github.com/stretchr/testify/suite"
 "pgregory.net/rapid"
)
func result()int{return audit.Add(1,1)}
func literal(int)int{return 2}
func wrapper(t *testing.T,v int){t.Helper();check.Equal(t,2,v)}
func recursive(n int)int{if n>0{return recursive(n-1)};return 2}
func opaque(string){}
func named()(n int){n=audit.Add(1,1);return}
func pair()(int,int){return audit.Add(1,1),2}
func set(r *audit.Record){r.N=audit.Add(1,1)}
type fakeGetter struct{}
func (fakeGetter) Get()int{return 2}
func TestInterface(t *testing.T){var x audit.Getter=fakeGetter{};check.Equal(t,2,x.Get())}
func TestLoopState(t *testing.T){got:=2;for range []int{1,2}{check.Equal(t,2,got);got=audit.Add(1,1)}}
func TestSeparateReturns(t *testing.T){_,n:=pair();check.Equal(t,2,n)}
func TestHelperWrite(t *testing.T){r:=audit.Record{};set(&r);check.Equal(t,2,r.N)}
func TestAliasWrite(t *testing.T){r:=audit.Record{};p:=&r;audit.Write(p);check.Equal(t,2,r.N)}
func TestOverwriteField(t *testing.T){r:=audit.Record{};r.N=audit.Add(1,1);r=audit.Record{};check.Equal(t,0,r.N)}
func TestConversion(t *testing.T){n:=audit.Add(1,1);check.Equal(t,2,int64(n))}
func TestMixed(t *testing.T){check.Equal(t,2,audit.Add(1,1));check.Equal(t,2,2)}
func TestHelperReturn(t *testing.T){check.Equal(t,2,result())}
func TestDiscardedHelperArgument(t *testing.T){check.Equal(t,2,literal(audit.Add(1,1)))}
func TestWrapper(t *testing.T){wrapper(t,audit.Add(1,1))}
func TestRecursion(t *testing.T){check.Equal(t,2,recursive(3))}
func TestNamedResult(t *testing.T){check.Equal(t,2,named())}
func TestExpressionAssertion(t *testing.T){if check.Equal(t,2,audit.Add(1,1)){return}}
func TestUnrelatedCall(t *testing.T){got:=audit.Add(1,1);opaque("hello");check.Equal(t,2,got)}
func TestRecorder(t *testing.T){r:=audit.Record{};audit.Write(&r);check.Equal(t,2,r.N)}
func TestAfter(t *testing.T){now:=time.Now();<-audit.Ready();check.True(t,time.Since(now)>0)}
func TestProperty(t *testing.T){rapid.Check(t,func(rt *rapid.T){if audit.Add(1,1)!=2{rt.Fatalf("bad")}})}
func TestStateMachine(t *testing.T){rapid.Check(t,func(rt *rapid.T){rt.Repeat(rapid.StateMachineActions(struct{}{}))})}
func ready()bool{return audit.Add(1,1)==2}
func TestNotPanicsCallback(t *testing.T){called:=false;check.NotPanics(t,func(){called=true});check.True(t,called)}
func TestEventually(t *testing.T){check.Eventually(t,func()bool{return audit.Add(1,1)==2},time.Second,time.Millisecond)}
func TestNamedCallback(t *testing.T){check.Eventually(t,ready,time.Second,time.Millisecond)}
func TestEventuallyWithT(t *testing.T){check.EventuallyWithT(t,func(c *check.CollectT){check.Equal(c,2,audit.Add(1,1))},time.Second,time.Millisecond)}
func TestClosure(t *testing.T){fn:=func()int{return audit.Add(1,1)};check.Equal(t,2,fn())}
func TestPackageLiteral(t *testing.T){check.Equal(t,2,support.Literal())}
func TestPackageProduction(t *testing.T){check.Equal(t,2,support.Production())}
func TestTable(t *testing.T){for _,n:=range []int{1,2}{t.Run("row",func(t *testing.T){check.Equal(t,n+1,audit.Add(n,1))})}}
func ExampleAdd(){_ = audit.Add(1,1)}
func FuzzAdd(f *testing.F){}
type Suite struct{suite.Suite;got int}
func TestSuite(t *testing.T){suite.Run(t,new(Suite))}
func (s *Suite) SetupTest(){s.init()}
func (s *Suite) init(){s.got=audit.Add(1,1)}
func (s *Suite) SetupSubTest(){s.got=2}
func (s *Suite) TestValue(){s.Equal(2,s.got);s.Require().Equal(2,s.got)}
func (s *Suite) TestLiteral(){s.Equal(2,2)}
func (s *Suite) TestSubtest(){s.Run("reset",func(){s.Equal(2,s.got)})}
`,
	}
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	findings, err := AnalyzeWithOptions(dir, []string{"."}, Options{HelperPackages: []string{"support/..."}})
	if err != nil {
		t.Fatal(err)
	}
	for _, pattern := range []string{"support/*", "support/.../bad", ""} {
		if _, err := AnalyzeWithOptions(dir, []string{"."}, Options{HelperPackages: []string{pattern}}); err == nil {
			t.Errorf("accepted invalid helper pattern %q", pattern)
		}
	}
	byName := map[string]Finding{}
	for _, f := range findings {
		if _, exists := byName[f.Test]; exists {
			t.Fatalf("duplicate entry %s", f.Test)
		}
		byName[f.Test] = f
	}
	want := map[string]string{
		"TestInterface": "questionable", "TestLoopState": "questionable", "TestSeparateReturns": "introverted", "TestHelperWrite": "extroverted", "TestAliasWrite": "questionable", "TestOverwriteField": "introverted", "TestConversion": "extroverted",
		"TestMixed": "extroverted", "TestHelperReturn": "extroverted", "TestDiscardedHelperArgument": "introverted", "TestWrapper": "extroverted",
		"TestRecursion": "questionable", "TestNamedResult": "extroverted", "TestExpressionAssertion": "extroverted", "TestUnrelatedCall": "extroverted",
		"TestRecorder": "questionable", "TestAfter": "questionable", "TestProperty": "extroverted", "TestStateMachine": "questionable",
		"TestNotPanicsCallback": "questionable", "TestEventually": "extroverted", "TestNamedCallback": "extroverted", "TestEventuallyWithT": "extroverted", "TestClosure": "extroverted",
		"TestPackageLiteral": "introverted", "TestPackageProduction": "extroverted", "TestTable": "container", "TestTable/row": "extroverted",
		"ExampleAdd": "questionable", "FuzzAdd": "questionable", "TestSuite": "container", "TestSuite/TestValue": "extroverted",
		"TestSuite/TestLiteral": "introverted", "TestSuite/TestSubtest": "container", "TestSuite/TestSubtest/reset": "introverted",
	}
	for name, verdict := range want {
		f, ok := byName[name]
		if !ok {
			t.Errorf("missing %s", name)
			continue
		}
		if f.Verdict != verdict {
			t.Errorf("%s: got %s, want %s: %+v", name, f.Verdict, verdict, f)
		}
	}
	if len(findings) != len(want) {
		t.Errorf("got %d entries, want %d", len(findings), len(want))
	}
	callback := byName["TestNotPanicsCallback"]
	if len(callback.Assertions) != 2 || callback.Assertions[1].Verdict != "questionable" {
		t.Errorf("unmodeled assertion callback lost its captured write: %+v", callback)
	}
	mixed := byName["TestMixed"]
	if len(mixed.Assertions) != 2 || mixed.Assertions[0].Verdict != "extroverted" || mixed.Assertions[1].Verdict != "introverted" {
		t.Errorf("mixed assertions hidden: %+v", mixed)
	}
	for _, name := range []string{"TestHelperReturn", "TestWrapper", "TestPackageProduction", "TestSuite/TestValue"} {
		f := byName[name]
		if len(f.Assertions) == 0 {
			continue
		}
		sources := f.Assertions[0].Sources
		if len(sources) == 0 || !strings.Contains(sources[0].Symbol, "Add") || !strings.HasSuffix(sources[0].File, "prod.go") || sources[0].Line != 2 {
			t.Errorf("%s: missing production endpoint: %+v", name, sources)
		}
	}
	for name, code := range map[string]string{"TestRecursion": "helper-limit", "TestRecorder": "reference-effect", "TestAfter": "channel-effect", "TestStateMachine": "rapid-state-machine", "FuzzAdd": "unsupported-entry"} {
		found := false
		for _, d := range byName[name].Diagnostics {
			if d.Code == code {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: missing diagnostic %s", name, code)
		}
	}
}

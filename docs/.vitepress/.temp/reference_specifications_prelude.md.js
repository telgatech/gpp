import { ssrRenderAttrs } from "vue/server-renderer";
import { useSSRContext } from "vue";
import { _ as _export_sfc } from "./plugin-vue_export-helper.1tPrXgE0.js";
const __pageData = JSON.parse('{"title":"==================================================2. Loading behavior","description":"","frontmatter":{},"headers":[],"relativePath":"reference/specifications/prelude.md","filePath":"reference/specifications/prelude.md","lastUpdated":null}');
const _sfc_main = { name: "reference/specifications/prelude.md" };
function _sfc_ssrRender(_ctx, _push, _parent, _attrs, $props, $setup, $data, $options) {
  _push(`<div${ssrRenderAttrs(_attrs)}><p>Go++ Feature Spec: Implicit prelude.gpp</p><p>Goal</p><p>Provide a small, automatically available Go++ prelude containing universally useful extensions and helpers so ordinary programs can be productive immediately without repetitive imports.</p><p>The prelude must remain:</p><ul><li>small</li><li>general-purpose</li><li>predictable</li><li>implemented in ordinary Go++ where possible</li><li>layered on top of native Go types</li><li>free of domain-specific frameworks</li></ul><p>Do NOT put ORM, web routing, validation frameworks, database abstractions, or application-specific behavior in the prelude.</p><p>==================================================</p><ol><li>File name ==================================================</li></ol><p>The built-in prelude source is:</p><p>prelude.gpp</p><p>It ships with the Go++ compiler/distribution.</p><p>Users do not need to import it.</p><h1 id="_2-loading-behavior" tabindex="-1">================================================== 2. Loading behavior <a class="header-anchor" href="#_2-loading-behavior" aria-label="Permalink to &quot;==================================================
2. Loading behavior&quot;">​</a></h1><p>Every Go++ package automatically has access to symbols and extension methods declared by prelude.gpp.</p><p>Conceptually:</p><p>package usercode</p><p>implicitly sees:</p><p>import/preload Go++ prelude</p><p>Do NOT textually concatenate prelude.gpp into source files.</p><p>Recommended implementation:</p><ol><li>Parse/type-check prelude.gpp once.</li><li>Cache its semantic representation.</li><li>Make exported prelude symbols/extensions visible in every Go++ package.</li><li>Compile required generated Go alongside user code.</li></ol><h1 id="_3-go-standard-library-remains-separate" tabindex="-1">================================================== 3. Go standard library remains separate <a class="header-anchor" href="#_3-go-standard-library-remains-separate" aria-label="Permalink to &quot;==================================================
3. Go standard library remains separate&quot;">​</a></h1><p>The prelude does NOT replace Go&#39;s standard library.</p><p>Users still write:</p><p>import ( &quot;fmt&quot; &quot;net/http&quot; &quot;database/sql&quot; )</p><p>when they need those packages.</p><p>The model is:</p><p>Go stdlib native Go packages</p><p>Go++ prelude implicit convenience helpers/extensions</p><p>Optional Go++ libraries explicitly imported packages such as model/web/validate</p><h1 id="_4-design-rule" tabindex="-1">================================================== 4. Design rule <a class="header-anchor" href="#_4-design-rule" aria-label="Permalink to &quot;==================================================
4. Design rule&quot;">​</a></h1><p>Only include functionality that is broadly useful across nearly all application domains.</p><p>Good candidates:</p><ul><li>slice helpers</li><li>map helpers</li><li>string helpers</li><li>small collection algorithms</li><li>sorting conveniences</li><li>min/max/clamp where useful</li><li>generic utility helpers</li></ul><p>Bad candidates:</p><ul><li>ORM Model</li><li>database CRUD</li><li>HTTP routing</li><li>authentication</li><li>form validation rules</li><li>JSON framework wrappers</li><li>logging framework</li><li>configuration framework</li><li>dependency injection</li></ul><p>Those should live in separate libraries.</p><h1 id="_5-native-go-types-only" tabindex="-1">================================================== 5. Native Go types only <a class="header-anchor" href="#_5-native-go-types-only" aria-label="Permalink to &quot;==================================================
5. Native Go types only&quot;">​</a></h1><p>Do not invent replacement collection types.</p><p>Use normal Go:</p><p>[]T map[K]V string</p><p>Enhance them with extension methods.</p><p>Example:</p><p>users.Sort(...) items.Any(...) name.Empty()</p><p>The underlying values remain ordinary Go values.</p><h1 id="_6-initial-slice-extension-set" tabindex="-1">================================================== 6. Initial slice extension set <a class="header-anchor" href="#_6-initial-slice-extension-set" aria-label="Permalink to &quot;==================================================
6. Initial slice extension set&quot;">​</a></h1><p>Provide a useful baseline for slices.</p><p>Conceptual API:</p><p>extend []T { func Any(fn func(T) bool) bool func All(fn func(T) bool) bool</p><pre><code>func Find(fn func(T) bool) (T, bool)

func Filter(fn func(T) bool) []T

func Contains(value T) bool
    where T is comparable

func Index(value T) int
    where T is comparable

func Reverse()

func Sort(less func(T, T) bool)
</code></pre><p>}</p><p>Exact generic constraint syntax should follow whatever Go++ currently supports.</p><p>If generic extension constraints are not yet implemented, split implementations using available Go generic mechanisms or defer constrained methods.</p><h1 id="_7-any" tabindex="-1">================================================== 7. Any <a class="header-anchor" href="#_7-any" aria-label="Permalink to &quot;==================================================
7. Any&quot;">​</a></h1><p>Example:</p><p>if users.Any(func(u User) bool { return u.Active }) { ... }</p><p>Semantics:</p><p>return true if predicate returns true for at least one element.</p><p>Short-circuit on first match.</p><p>Equivalent conceptual implementation:</p><p>for _, v := range this { if fn(v) { return true } }</p><p>return false</p><h1 id="_8-all" tabindex="-1">================================================== 8. All <a class="header-anchor" href="#_8-all" aria-label="Permalink to &quot;==================================================
8. All&quot;">​</a></h1><p>Example:</p><p>if users.All(func(u User) bool { return u.Active }) { ... }</p><p>Return true only if predicate is true for all elements.</p><p>Empty slice returns true.</p><p>Short-circuit on first false.</p><h1 id="_9-find" tabindex="-1">================================================== 9. Find <a class="header-anchor" href="#_9-find" aria-label="Permalink to &quot;==================================================
9. Find&quot;">​</a></h1><p>Example:</p><p>user, ok := users.Find(func(u User) bool { return u.Id == id })</p><p>Recommended return:</p><p>(T, bool)</p><p>rather than pointer-to-element.</p><p>Reason:</p><ul><li>works for all element types</li><li>mirrors common Go idiom</li><li>avoids pointer/address lifetime complications</li><li>preserves ordinary value semantics</li></ul><p>First matching element wins.</p><p>If absent:</p><p>zero(T), false</p><h1 id="_10-filter" tabindex="-1">================================================== 10. Filter <a class="header-anchor" href="#_10-filter" aria-label="Permalink to &quot;==================================================
10. Filter&quot;">​</a></h1><p>Example:</p><p>active := users.Filter(func(u User) bool { return u.Active })</p><p>Return a new []T containing matching elements in original order.</p><p>Do not mutate original slice.</p><h1 id="_11-contains" tabindex="-1">================================================== 11. Contains <a class="header-anchor" href="#_11-contains" aria-label="Permalink to &quot;==================================================
11. Contains&quot;">​</a></h1><p>For comparable T:</p><p>if names.Contains(&quot;Bob&quot;) { ... }</p><p>Use ordinary equality semantics.</p><p>Could delegate to slices.Contains where available.</p><h1 id="_12-index" tabindex="-1">================================================== 12. Index <a class="header-anchor" href="#_12-index" aria-label="Permalink to &quot;==================================================
12. Index&quot;">​</a></h1><p>For comparable T:</p><p>i := names.Index(&quot;Bob&quot;)</p><p>Return:</p><p>index &gt;= 0 if found -1 otherwise</p><p>Could delegate to slices.Index where available.</p><h1 id="_13-reverse" tabindex="-1">================================================== 13. Reverse <a class="header-anchor" href="#_13-reverse" aria-label="Permalink to &quot;==================================================
13. Reverse&quot;">​</a></h1><p>Example:</p><p>users.Reverse()</p><p>Recommended semantics:</p><p>mutates the slice in place.</p><p>Reason:</p><p>this maps naturally to slices.Reverse.</p><p>If a copying variant is desired later, add:</p><p>Reversed()</p><p>Do not make Reverse silently allocate.</p><h1 id="_14-sort-with-comparator" tabindex="-1">================================================== 14. Sort with comparator <a class="header-anchor" href="#_14-sort-with-comparator" aria-label="Permalink to &quot;==================================================
14. Sort with comparator&quot;">​</a></h1><p>Example:</p><p>users.Sort(func(a, b User) bool { return a.Name &lt; b.Name })</p><p>Semantics:</p><p>sort slice in place.</p><p>Comparator returns true when a should sort before b.</p><p>Lower using slices.SortFunc or sort.Slice depending on generated Go/version.</p><p>If using slices.SortFunc, adapt bool comparator to cmp-style result as needed.</p><h1 id="_15-ordered-slice-helpers" tabindex="-1">================================================== 15. Ordered slice helpers <a class="header-anchor" href="#_15-ordered-slice-helpers" aria-label="Permalink to &quot;==================================================
15. Ordered slice helpers&quot;">​</a></h1><p>For ordered element types, provide:</p><p>numbers.Sort() numbers.SortDesc() numbers.Min() numbers.Max()</p><p>Examples:</p><p>names.Sort()</p><p>scores.SortDesc()</p><p>smallest := scores.Min() largest := scores.Max()</p><p>Use Go&#39;s cmp.Ordered/slices helpers where practical.</p><p>If overloading allows:</p><p>Sort()</p><p>and:</p><p>Sort(func(T,T) bool)</p><p>both may coexist naturally.</p><h1 id="_16-sortdesc" tabindex="-1">================================================== 16. SortDesc <a class="header-anchor" href="#_16-sortdesc" aria-label="Permalink to &quot;==================================================
16. SortDesc&quot;">​</a></h1><p>For ordered T:</p><p>values.SortDesc()</p><p>Mutates slice in place.</p><p>Equivalent to descending natural ordering.</p><h1 id="_17-min-max" tabindex="-1">================================================== 17. Min / Max <a class="header-anchor" href="#_17-min-max" aria-label="Permalink to &quot;==================================================
17. Min / Max&quot;">​</a></h1><p>For ordered T:</p><p>min := values.Min() max := values.Max()</p><p>Need a defined empty-slice behavior.</p><p>Recommended:</p><p>panic/exception on empty input is undesirable.</p><p>Prefer:</p><p>func Min() (T, bool) func Max() (T, bool)</p><p>Example:</p><p>min, ok := values.Min()</p><p>This is more Go-like and avoids hidden exceptional behavior.</p><h1 id="_18-string-helpers" tabindex="-1">================================================== 18. String helpers <a class="header-anchor" href="#_18-string-helpers" aria-label="Permalink to &quot;==================================================
18. String helpers&quot;">​</a></h1><p>Provide only genuinely useful tiny helpers.</p><p>Initial candidates:</p><p>extend string { func Empty() bool func Blank() bool }</p><p><code>Empty</code>:</p><p>return len(this) == 0</p><p><code>Blank</code>:</p><p>true if string is empty or contains only Unicode whitespace.</p><p>Use strings.TrimSpace or equivalent.</p><p>Example:</p><p>if name.Blank() { ... }</p><p>Avoid adding dozens of Rails-style string methods initially.</p><h1 id="_19-map-helpers" tabindex="-1">================================================== 19. Map helpers <a class="header-anchor" href="#_19-map-helpers" aria-label="Permalink to &quot;==================================================
19. Map helpers&quot;">​</a></h1><p>Useful map helpers may include:</p><p>extend map[K]V { func Keys() []K func Values() []V func Has(key K) bool }</p><p>Examples:</p><p>keys := users.Keys() values := users.Values()</p><p>if users.Has(id) { ... }</p><p><code>Has</code> is convenience over:</p><p>_, ok := m[key]</p><p>Keys/Values ordering remains unspecified, matching Go map iteration semantics.</p><h1 id="_20-getor" tabindex="-1">================================================== 20. GetOr <a class="header-anchor" href="#_20-getor" aria-label="Permalink to &quot;==================================================
20. GetOr&quot;">​</a></h1><p>Potentially useful map helper:</p><p>extend map[K]V { func GetOr(key K, fallback V) V }</p><p>Example:</p><p>port := config.GetOr(&quot;port&quot;, &quot;8080&quot;)</p><p>Must return fallback only if key is absent.</p><p>Do not confuse absent with zero-valued stored values.</p><p>Implementation:</p><p>v, ok := this[key] if !ok { return fallback } return v</p><h1 id="_21-generic-numeric-helpers" tabindex="-1">================================================== 21. Generic numeric helpers <a class="header-anchor" href="#_21-generic-numeric-helpers" aria-label="Permalink to &quot;==================================================
21. Generic numeric helpers&quot;">​</a></h1><p>Potentially include:</p><p>Min(a, b) Max(a, b) Clamp(value, min, max)</p><p>only if they are not already ergonomically available from Go packages.</p><p>Because the prelude should remain small, these may be deferred.</p><h1 id="_22-no-implicit-it" tabindex="-1">================================================== 22. No implicit <code>it</code> <a class="header-anchor" href="#_22-no-implicit-it" aria-label="Permalink to &quot;==================================================
22. No implicit \`it\`&quot;">​</a></h1><p>Do NOT require implicit <code>it</code> or lambda shorthand for prelude APIs.</p><p>Use normal functions initially:</p><p>users.Filter(func(u User) bool { return u.Active })</p><p>If Go++ later gains concise function syntax, these APIs automatically become nicer.</p><p>Do not make the prelude depend on speculative language features.</p><h1 id="_23-implementation-language" tabindex="-1">================================================== 23. Implementation language <a class="header-anchor" href="#_23-implementation-language" aria-label="Permalink to &quot;==================================================
23. Implementation language&quot;">​</a></h1><p>Prefer implementing prelude.gpp in Go++ itself.</p><p>Example:</p><p>extend []T { func Any(fn func(T) bool) bool { for _, v := range this { if fn(v) { return true } } return false } }</p><p>This serves two purposes:</p><ol><li>useful standard conveniences</li><li>a real-world stress test/demo of Go++ features</li></ol><p>Use native Go interop where useful.</p><h1 id="_24-prelude-dependencies" tabindex="-1">================================================== 24. Prelude dependencies <a class="header-anchor" href="#_24-prelude-dependencies" aria-label="Permalink to &quot;==================================================
24. Prelude dependencies&quot;">​</a></h1><p>prelude.gpp may import selected Go stdlib packages internally, such as:</p><p>&quot;slices&quot; &quot;strings&quot; &quot;cmp&quot;</p><p>These dependencies should be compiler-managed.</p><p>Users should not need to import those packages merely because the prelude implementation uses them.</p><h1 id="_25-name-collision-rules" tabindex="-1">================================================== 25. Name collision rules <a class="header-anchor" href="#_25-name-collision-rules" aria-label="Permalink to &quot;==================================================
25. Name collision rules&quot;">​</a></h1><p>Real/native methods always beat prelude extensions according to normal extension resolution.</p><p>If user code defines a conflicting extension with equal precedence, follow normal extension ambiguity rules.</p><p>Do not silently let the prelude override user-defined behavior.</p><p>Recommended precedence:</p><ol><li>real/native methods</li><li>Go++ class methods</li><li>explicitly imported/user extension methods</li><li>prelude extension methods</li></ol><p>This gives user code an opportunity to supply a more specific extension without being trapped by the prelude.</p><p>If current extension resolution does not distinguish explicit vs prelude extensions, add origin metadata.</p><h1 id="_26-shadowing-ordinary-functions" tabindex="-1">================================================== 26. Shadowing ordinary functions <a class="header-anchor" href="#_26-shadowing-ordinary-functions" aria-label="Permalink to &quot;==================================================
26. Shadowing ordinary functions&quot;">​</a></h1><p>Prelude package-level functions should enter normal symbol resolution carefully.</p><p>Avoid generic names likely to collide heavily.</p><p>Prefer extension methods where possible.</p><p>Example:</p><p>values.Sort()</p><p>is better than globally injecting:</p><p>Sort(values)</p><p>The prelude should not pollute package namespaces unnecessarily.</p><h1 id="_27-disable-option" tabindex="-1">================================================== 27. Disable option <a class="header-anchor" href="#_27-disable-option" aria-label="Permalink to &quot;==================================================
27. Disable option&quot;">​</a></h1><p>Provide a compiler option for testing/minimal builds:</p><p>gpp build --no-prelude</p><p>gpp run --no-prelude</p><p>When disabled:</p><ul><li>no prelude symbols</li><li>no prelude extensions</li><li>normal Go++ language remains available</li></ul><p>This is useful for:</p><ul><li>compiler tests</li><li>bootstrapping</li><li>diagnosing conflicts</li><li>minimal generated output</li></ul><h1 id="_28-versioning" tabindex="-1">================================================== 28. Versioning <a class="header-anchor" href="#_28-versioning" aria-label="Permalink to &quot;==================================================
28. Versioning&quot;">​</a></h1><p>The prelude is versioned with the Go++ compiler.</p><p>Do not fetch a remote prelude during compilation.</p><p>Compiler distribution contains the canonical prelude.gpp.</p><p>Programs therefore get deterministic prelude behavior for a given Go++ compiler version.</p><h1 id="_29-source-availability" tabindex="-1">================================================== 29. Source availability <a class="header-anchor" href="#_29-source-availability" aria-label="Permalink to &quot;==================================================
29. Source availability&quot;">​</a></h1><p>Ship prelude.gpp as readable source.</p><p>Users should be able to inspect it.</p><p>It should demonstrate idiomatic Go++.</p><p>Avoid hiding most of the implementation in compiler intrinsics unless required for correctness/performance.</p><h1 id="_30-intrinsics" tabindex="-1">================================================== 30. Intrinsics <a class="header-anchor" href="#_30-intrinsics" aria-label="Permalink to &quot;==================================================
30. Intrinsics&quot;">​</a></h1><p>Some operations may require compiler support, but keep intrinsics minimal.</p><p>For example:</p><ul><li>language operators</li><li>reflection descriptors</li><li>atomic ++/--</li><li>class runtime machinery</li></ul><p>should remain compiler features.</p><p>Collection algorithms should not become compiler intrinsics merely for convenience.</p><h1 id="_31-suggested-initial-prelude-gpp" tabindex="-1">================================================== 31. Suggested initial prelude.gpp <a class="header-anchor" href="#_31-suggested-initial-prelude-gpp" aria-label="Permalink to &quot;==================================================
31. Suggested initial prelude.gpp&quot;">​</a></h1><p>Conceptually:</p><p>package prelude</p><p>import ( &quot;slices&quot; &quot;strings&quot; )</p><p>extend []T { func Any(fn func(T) bool) bool { for _, v := range this { if fn(v) { return true } } return false }</p><pre><code>func All(fn func(T) bool) bool {
    for _, v := range this {
        if !fn(v) {
            return false
        }
    }
    return true
}

func Find(fn func(T) bool) (T, bool) {
    for _, v := range this {
        if fn(v) {
            return v, true
        }
    }

    var zero T
    return zero, false
}

func Filter(fn func(T) bool) []T {
    out := []T{}

    for _, v := range this {
        if fn(v) {
            out = append(out, v)
        }
    }

    return out
}

func Reverse() {
    slices.Reverse(this)
}

func Sort(less func(T, T) bool) {
    slices.SortFunc(this, func(a, b T) int {
        if less(a, b) {
            return -1
        }
        if less(b, a) {
            return 1
        }
        return 0
    })
}
</code></pre><p>}</p><p>extend string { func Empty() bool { return len(this) == 0 }</p><pre><code>func Blank() bool {
    return len(strings.TrimSpace(this)) == 0
}
</code></pre><p>}</p><p>The exact syntax may need adjustment to current generic-extension support.</p><h1 id="_32-additional-ordered-extensions" tabindex="-1">================================================== 32. Additional ordered extensions <a class="header-anchor" href="#_32-additional-ordered-extensions" aria-label="Permalink to &quot;==================================================
32. Additional ordered extensions&quot;">​</a></h1><p>Where constraints are supported:</p><p>extend []T where T cmp.Ordered { func Sort() { slices.Sort(this) }</p><pre><code>func SortDesc() {
    slices.Sort(this)
    slices.Reverse(this)
}

func Min() (T, bool) {
    if len(this) == 0 {
        var zero T
        return zero, false
    }

    return slices.Min(this), true
}

func Max() (T, bool) {
    if len(this) == 0 {
        var zero T
        return zero, false
    }

    return slices.Max(this), true
}
</code></pre><p>}</p><p>If Go++ does not yet support this constraint syntax, implement later rather than adding ad-hoc compiler behavior.</p><h1 id="_33-suggested-v1-contents" tabindex="-1">================================================== 33. Suggested v1 contents <a class="header-anchor" href="#_33-suggested-v1-contents" aria-label="Permalink to &quot;==================================================
33. Suggested v1 contents&quot;">​</a></h1><p>Ship v1 with approximately:</p><p>Slices: Any All Find Filter Contains Index Reverse Sort(comparator)</p><p>Ordered slices: Sort() SortDesc() Min() Max()</p><p>Strings: Empty Blank</p><p>Maps: Keys Values Has GetOr</p><p>Keep v1 intentionally small.</p><h1 id="_34-features-not-in-v1-prelude" tabindex="-1">================================================== 34. Features NOT in v1 prelude <a class="header-anchor" href="#_34-features-not-in-v1-prelude" aria-label="Permalink to &quot;==================================================
34. Features NOT in v1 prelude&quot;">​</a></h1><p>Do not include:</p><p>Reduce GroupBy Chunk Zip DistinctBy ParallelMap Retry Memoize ORM helpers HTTP helpers validation rules</p><p>These may live in optional libraries if demand appears.</p><p>The prelude should not turn into a kitchen-sink utility framework.</p><h1 id="_35-tests" tabindex="-1">================================================== 35. Tests <a class="header-anchor" href="#_35-tests" aria-label="Permalink to &quot;==================================================
35. Tests&quot;">​</a></h1><p>Implicit availability:</p><p>package main</p><p 1,2,3="">func main() { xs := []int</p><pre><code>assert(xs.Any(func(x int) bool {
    return x == 2
}))
</code></pre><p>}</p><p>must compile without importing prelude.</p><p>Find:</p><p>v, ok := xs.Find(...)</p><p>must return first match.</p><p>Filter:</p><p>must preserve order.</p><p>Reverse:</p><p>must mutate in place.</p><p>Sort:</p><p>must mutate in place and honor comparator.</p><p>Empty:</p><p>&quot;&quot;.Empty() == true &quot;x&quot;.Empty() == false</p><p>Blank:</p><p>&quot; &quot;.Blank() == true &quot;\\n\\t&quot;.Blank() == true &quot;x&quot;.Blank() == false</p><p>Maps:</p><p>m.Has(k) m.Keys() m.Values() m.GetOr(k, fallback)</p><p>must behave according to normal Go map semantics.</p><p>No-prelude:</p><p>gpp build --no-prelude</p><p>must make prelude extension calls unresolved.</p><h1 id="_36-design-principle" tabindex="-1">================================================== 36. Design principle <a class="header-anchor" href="#_36-design-principle" aria-label="Permalink to &quot;==================================================
36. Design principle&quot;">​</a></h1><p>prelude.gpp should make Go++ pleasant immediately without creating a second standard library.</p><p>Use native Go types. Use native Go packages underneath. Add only small universal conveniences.</p><p>If a feature can be implemented as ordinary Go++ extension code, prefer that over compiler magic.</p><p>The ideal experience is:</p><p>users.Sort(...) users.Filter(...) users.Any(...) name.Blank() config.GetOr(...)</p><p>with zero imports and zero loss of Go interoperability.</p></div>`);
}
const _sfc_setup = _sfc_main.setup;
_sfc_main.setup = (props, ctx) => {
  const ssrContext = useSSRContext();
  (ssrContext.modules || (ssrContext.modules = /* @__PURE__ */ new Set())).add("reference/specifications/prelude.md");
  return _sfc_setup ? _sfc_setup(props, ctx) : void 0;
};
const prelude = /* @__PURE__ */ _export_sfc(_sfc_main, [["ssrRender", _sfc_ssrRender]]);
export {
  __pageData,
  prelude as default
};

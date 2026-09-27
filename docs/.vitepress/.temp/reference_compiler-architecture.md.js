import { ssrRenderAttrs } from "vue/server-renderer";
import { useSSRContext } from "vue";
import { _ as _export_sfc } from "./plugin-vue_export-helper.1tPrXgE0.js";
const __pageData = JSON.parse('{"title":"Compiler architecture","description":"","frontmatter":{},"headers":[],"relativePath":"reference/compiler-architecture.md","filePath":"reference/compiler-architecture.md","lastUpdated":null}');
const _sfc_main = { name: "reference/compiler-architecture.md" };
function _sfc_ssrRender(_ctx, _push, _parent, _attrs, $props, $setup, $data, $options) {
  _push(`<div${ssrRenderAttrs(_attrs)}><h1 id="compiler-architecture" tabindex="-1">Compiler architecture <a class="header-anchor" href="#compiler-architecture" aria-label="Permalink to &quot;Compiler architecture&quot;">​</a></h1><p>Go++ uses a dedicated frontend because its syntax extends Go. The compiler parses Go++ constructs into typed AST nodes, transforms those nodes into Go AST where possible, and emits ordinary Go for the final toolchain.</p><h2 id="the-pipeline" tabindex="-1">The pipeline <a class="header-anchor" href="#the-pipeline" aria-label="Permalink to &quot;The pipeline&quot;">​</a></h2><div class="language-text vp-adaptive-theme"><button title="Copy Code" class="copy"></button><span class="lang">text</span><pre class="shiki shiki-themes github-light github-dark vp-code" tabindex="0"><code><span class="line"><span>source .gpp</span></span>
<span class="line"><span>    ↓</span></span>
<span class="line"><span>lossless lexer and Go++ parser</span></span>
<span class="line"><span>    ↓</span></span>
<span class="line"><span>typed Go++ AST with source spans</span></span>
<span class="line"><span>    ↓</span></span>
<span class="line"><span>semantic analysis and feature lowering</span></span>
<span class="line"><span>    ↓</span></span>
<span class="line"><span>Go AST / generated Go declarations</span></span>
<span class="line"><span>    ↓</span></span>
<span class="line"><span>go build, go test, or go run</span></span></code></pre></div><p>The AST is not just an implementation detail. It powers diagnostics, formatting, LSP features, overload resolution, source mapping, and generated documentation.</p><h2 id="why-not-patch-the-go-compiler" tabindex="-1">Why not patch the Go compiler? <a class="header-anchor" href="#why-not-patch-the-go-compiler" aria-label="Permalink to &quot;Why not patch the Go compiler?&quot;">​</a></h2><p>Go’s compiler internals are not a stable extension API, and its parser rejects Go++ syntax before an extension could transform it. A dedicated frontend lets Go++ evolve its syntax while preserving the important interoperability point: the output is ordinary Go.</p><h2 id="source-fidelity" tabindex="-1">Source fidelity <a class="header-anchor" href="#source-fidelity" aria-label="Permalink to &quot;Source fidelity&quot;">​</a></h2><p>Executable syntax is represented structurally. Source text is retained only where exact text is meaningful, such as comments, raw strings, templates, embedded files, and diagnostics. This keeps transformations composable and lets tooling report locations in the original Go++ source.</p><p>Read the <a href="./specifications/compiler.ast">full AST specification</a> for the invariants that guide the implementation.</p></div>`);
}
const _sfc_setup = _sfc_main.setup;
_sfc_main.setup = (props, ctx) => {
  const ssrContext = useSSRContext();
  (ssrContext.modules || (ssrContext.modules = /* @__PURE__ */ new Set())).add("reference/compiler-architecture.md");
  return _sfc_setup ? _sfc_setup(props, ctx) : void 0;
};
const compilerArchitecture = /* @__PURE__ */ _export_sfc(_sfc_main, [["ssrRender", _sfc_ssrRender]]);
export {
  __pageData,
  compilerArchitecture as default
};

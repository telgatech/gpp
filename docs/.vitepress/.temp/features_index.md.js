import { ssrRenderAttrs } from "vue/server-renderer";
import { useSSRContext } from "vue";
import { _ as _export_sfc } from "./plugin-vue_export-helper.1tPrXgE0.js";
const __pageData = JSON.parse('{"title":"Go++ features","description":"","frontmatter":{},"headers":[],"relativePath":"features/index.md","filePath":"features/index.md","lastUpdated":null}');
const _sfc_main = { name: "features/index.md" };
function _sfc_ssrRender(_ctx, _push, _parent, _attrs, $props, $setup, $data, $options) {
  _push(`<div${ssrRenderAttrs(_attrs)}><h1 id="go-features" tabindex="-1">Go++ features <a class="header-anchor" href="#go-features" aria-label="Permalink to &quot;Go++ features&quot;">​</a></h1><p>Go++ adds language features for application code and builds on familiar Go packages, libraries, and tools. Each guide below introduces one feature, compares it with the Go approach where useful, and then shows how to apply it.</p><h2 id="core-language-changes" tabindex="-1">Core language changes <a class="header-anchor" href="#core-language-changes" aria-label="Permalink to &quot;Core language changes&quot;">​</a></h2><ul><li><a href="./classes">Classes</a></li><li><a href="./polymorphism">Polymorphism</a></li><li><a href="./overloading">Function and method overloading</a></li><li><a href="./multiple-inheritance">Multiple inheritance</a></li><li><a href="./exceptions">Exception handling</a></li><li><a href="./extensions">Extension methods</a></li><li><a href="./serialization">Serialization</a></li><li><a href="./construction">Class construction</a></li><li><a href="./static-methods">Static methods and factories</a></li><li><a href="./named-arguments">Named arguments and defaults</a></li><li><a href="./records">Structural records</a></li><li><a href="./enums">Enums</a></li><li><a href="./interpolation">String interpolation</a></li><li><a href="./lambdas">Lambdas</a></li><li><a href="./regular-expressions">Regular expressions</a></li><li><a href="./safe-access">Safe access</a></li><li><a href="./error-fallback">Lazy error fallback</a></li><li><a href="./metadata">Annotations and introspection</a></li></ul><h2 id="application-features" tabindex="-1">Application features <a class="header-anchor" href="#application-features" aria-label="Permalink to &quot;Application features&quot;">​</a></h2><ul><li><a href="./templates">Typed templates</a></li><li><a href="./embedded-assets">Embedded assets</a></li><li><a href="./http">HTTP servers and routes</a></li><li><a href="./api-documentation">OpenAPI, Swagger, and OAuth</a></li><li><a href="./orm">ORM and SQL</a></li><li><a href="./testing">Suite-based tests</a></li><li><a href="./template-reload">External template reloads</a></li></ul><h2 id="go-workflow-and-tools" tabindex="-1">Go workflow and tools <a class="header-anchor" href="#go-workflow-and-tools" aria-label="Permalink to &quot;Go workflow and tools&quot;">​</a></h2><ul><li><a href="./packages">Go compatibility</a></li><li><a href="./mixed-go">Mixed Go and Go++ builds</a></li><li><a href="./project-cli">Project CLI</a></li><li><a href="./formatter">Formatter</a></li><li><a href="./source-docs">Source documentation</a></li><li><a href="./language-server">Language server and diagnostics</a></li></ul></div>`);
}
const _sfc_setup = _sfc_main.setup;
_sfc_main.setup = (props, ctx) => {
  const ssrContext = useSSRContext();
  (ssrContext.modules || (ssrContext.modules = /* @__PURE__ */ new Set())).add("features/index.md");
  return _sfc_setup ? _sfc_setup(props, ctx) : void 0;
};
const index = /* @__PURE__ */ _export_sfc(_sfc_main, [["ssrRender", _sfc_ssrRender]]);
export {
  __pageData,
  index as default
};

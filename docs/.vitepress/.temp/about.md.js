import { ssrRenderAttrs, ssrRenderComponent } from "vue/server-renderer";
import { ref, onMounted, onBeforeUnmount, mergeProps, useSSRContext } from "vue";
import { _ as _export_sfc } from "./plugin-vue_export-helper.1tPrXgE0.js";
const _sfc_main$1 = {
  __name: "DonationButton",
  __ssrInlineRender: true,
  setup(__props) {
    const form = ref(null);
    let paymentButtonScript;
    onMounted(() => {
      var _a;
      paymentButtonScript = document.createElement("script");
      paymentButtonScript.src = "https://checkout.razorpay.com/v1/payment-button.js";
      paymentButtonScript.setAttribute("data-payment_button_id", "pl_Tgy6a0IM1LKQJV");
      paymentButtonScript.async = true;
      (_a = form.value) == null ? void 0 : _a.append(paymentButtonScript);
    });
    onBeforeUnmount(() => {
      paymentButtonScript == null ? void 0 : paymentButtonScript.remove();
    });
    return (_ctx, _push, _parent, _attrs) => {
      _push(`<form${ssrRenderAttrs(mergeProps({
        ref_key: "form",
        ref: form,
        class: "donation-button"
      }, _attrs))} data-v-63f8c9a6></form>`);
    };
  }
};
const _sfc_setup$1 = _sfc_main$1.setup;
_sfc_main$1.setup = (props, ctx) => {
  const ssrContext = useSSRContext();
  (ssrContext.modules || (ssrContext.modules = /* @__PURE__ */ new Set())).add(".vitepress/theme/DonationButton.vue");
  return _sfc_setup$1 ? _sfc_setup$1(props, ctx) : void 0;
};
const DonationButton = /* @__PURE__ */ _export_sfc(_sfc_main$1, [["__scopeId", "data-v-63f8c9a6"]]);
const __pageData = JSON.parse('{"title":"About Go++","description":"","frontmatter":{},"headers":[],"relativePath":"about.md","filePath":"about.md","lastUpdated":null}');
const __default__ = { name: "about.md" };
const _sfc_main = /* @__PURE__ */ Object.assign(__default__, {
  __ssrInlineRender: true,
  setup(__props) {
    return (_ctx, _push, _parent, _attrs) => {
      _push(`<div${ssrRenderAttrs(_attrs)}><h1 id="about-go" tabindex="-1">About Go++ <a class="header-anchor" href="#about-go" aria-label="Permalink to &quot;About Go++&quot;">​</a></h1><div class="about-profile-card"><img class="about-profile-avatar" src="https://media.licdn.com/dms/image/v2/D5603AQHyajRXSjE7ng/profile-displayphoto-shrink_200_200/profile-displayphoto-shrink_200_200/0/1722577204929?e=1792022400&amp;v=beta&amp;t=P_-cFL_q3ZyrVPjWY3AAEE3OalxXjkNv3cRup0Yrluk" alt="Sunder Rajan&#39;s LinkedIn profile photo" width="88" height="88" loading="lazy"><div class="about-profile-copy"><h2>Sunder Rajan</h2><p>Creator of Go++ · Telga Technologies</p><a href="https://in.linkedin.com/in/sunder-rajan-1a4154171" target="_blank" rel="noreferrer"> LinkedIn · See my other projects <span aria-hidden="true">→</span></a></div></div><h2 id="why-i-created-go" tabindex="-1">Why I created Go++ <a class="header-anchor" href="#why-i-created-go" aria-label="Permalink to &quot;Why I created Go++&quot;">​</a></h2><p>I&#39;ve been programming in Go since the early 1.0 days. I love the productivity it brings: the language is approachable, its tools are cohesive, and it is refreshing to build software on a platform that feels so dependable. Go is a remarkably reliable engineering achievement, and that is exactly why I want to build on it rather than replace it.</p><p>Go&#39;s conservative approach to language features has helped keep the language small, stable, and consistent. At the same time, years of application development have left me wishing for less ceremony and more expressive ways to handle some common patterns. That tension is where Go++ began: an experiment to see whether those annoyances can be addressed while keeping Go&#39;s character, toolchain, and interoperability intact.</p><p>Go++ explores that idea by compiling to ordinary Go, keeping existing Go packages and tools part of the story. I build it through <a href="https://telga.in" target="_blank" rel="noreferrer">Telga</a>.</p><h2 id="experimental-status" tabindex="-1">Experimental status <a class="header-anchor" href="#experimental-status" aria-label="Permalink to &quot;Experimental status&quot;">​</a></h2><p>Go++ is an experiment to see how far Go can be pushed to address the major annoyances people raise while preserving the language&#39;s character. I’m not a compiler developer, and this project has not yet been broadly vetted by the Go community. Until it has received that scrutiny and earned confidence through real-world use, please don’t adopt it casually for production systems.</p><h2 id="consulting-and-project-support" tabindex="-1">Consulting and project support <a class="header-anchor" href="#consulting-and-project-support" aria-label="Permalink to &quot;Consulting and project support&quot;">​</a></h2><p>I’m open to consulting engagements. If you’re exploring Go++ or need help with a software project, get in touch through <a href="https://telga.in" target="_blank" rel="noreferrer">Telga</a> or <a href="https://in.linkedin.com/in/sunder-rajan-1a4154171" target="_blank" rel="noreferrer">LinkedIn</a>.</p><p>I’m also open to donations to help keep Go++ moving. If you’d like to support the project, you can contribute here:</p>`);
      _push(ssrRenderComponent(DonationButton, null, null, _parent));
      _push(`</div>`);
    };
  }
});
const _sfc_setup = _sfc_main.setup;
_sfc_main.setup = (props, ctx) => {
  const ssrContext = useSSRContext();
  (ssrContext.modules || (ssrContext.modules = /* @__PURE__ */ new Set())).add("about.md");
  return _sfc_setup ? _sfc_setup(props, ctx) : void 0;
};
export {
  __pageData,
  _sfc_main as default
};

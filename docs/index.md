---
layout: home

hero:
  text: Go++
  tagline: "Extends Go with classes, inheritance, generics, function overloading, named arguments, exceptions, extension methods, annotations, anonymous structs, enums and more!"
  image:
    src: /go-gopher-superman.webp
    alt: Go's blue gopher mascot flying upward with a red cape
  actions:
    - theme: brand
      text: Explore the features
      link: /features/classes
    - theme: alt
      text: Build your first project
      link: /guide/getting-started
---

<script setup>
import FeatureCatalog from './.vitepress/theme/FeatureCatalog.vue'
</script>

<footer class="catalog-footer latest-change-card">
<div class="catalog-footer-copy">
<p class="catalog-footer-eyebrow"><span class="latest-change-ribbon">What's new</span> 2026-11-05</p>
<h2>Typed database queries</h2>
<p>Use <code>gpp/sql</code> for typed scalar and struct queries; use <code>gpp/orm</code> for model-mapped queries and persistence.</p>
</div>
<div class="latest-change-actions">
<a href="/features/sql">Explore typed queries <span aria-hidden="true">→</span></a>
<a class="latest-change-changelog" href="https://github.com/telgatech/gpp/blob/main/CHANGELOG.md">See changelog</a>
</div>
</footer>

<FeatureCatalog />

<div class="home-faq-heading">
<h2 class="home-faq-title">Frequently asked questions</h2>
<p>Quick answers about Go++, its current status, and how to get involved.</p>
</div>

<div class="home-faq">

<details>
<summary>What is Go++?</summary>
<p>Go++ is an experiment in adding higher-level language features to Go while preserving Go’s familiar tools, packages, and runtime. It adds features such as classes, multiple inheritance, overloading, exceptions, and built-in serialization.</p>
</details>

<details>
<summary>Is Go++ an official Go project?</summary>
<p>No. Go++ is an independent project and is not affiliated with or endorsed by Google or the Go team.</p>
</details>

<details>
<summary>How does Go++ work with Go?</summary>
<p>Go++ lowers to ordinary Go, so projects can use existing Go packages and interoperate with Go code. Compile <code>.gpp</code> files with the <code>gpp</code> CLI; the Go toolchain then builds and runs the generated Go. <a href="/guide/getting-started">Get started</a> or browse the <a href="/examples/">complete examples</a>.</p>
</details>

<details>
<summary>Can I adopt Go++ gradually?</summary>
<p>Yes. Start with familiar Go syntax and compile <code>.go</code> files with <code>gpp run</code>. Add Go++ features like classes or named arguments when they help, and keep existing Go code alongside it. See <a href="/features/mixed-go">how Go and Go++ work together</a>.</p>
</details>

<details>
<summary>Is Go++ ready for production?</summary>
<p>Go++ is still an experiment and has not yet received broad community review. Until then, please don’t adopt it casually for production systems.</p>
</details>

<details>
<summary>What license applies to Go++?</summary>
<p>Go++ uses the same BSD 3-Clause license as Go. You can use, modify, and redistribute Go++ in source or binary form, including commercially, as long as you follow the license conditions. See the <a href="https://github.com/telgatech/gpp/blob/main/LICENSE">full license</a>. Some bundled third-party components have their own notices and licenses.</p>
</details>

<details>
<summary>Can I contribute?</summary>
<p>For now, the best way to get involved is to share questions, ideas, and bug reports through <a href="https://github.com/telgatech/gpp/issues">GitHub Issues</a> or <a href="https://github.com/telgatech/gpp/discussions">GitHub Discussions</a>. For a larger feature idea, start a Discussion so we can explore the design together. I may open pull requests to outside contributors as the project matures.</p>
</details>

<details>
<summary>Are AI-assisted contributions welcome?</summary>
<p>This codebase already includes work made with large language models. If you use AI to develop an idea or report, share it through Issues or Discussions. Any proposed changes should advance the compiler or developer productivity without reducing quality or performance.</p>
</details>

<details>
<summary>How can I try Go++?</summary>
<p>Install the CLI with <code>go install github.com/telgatech/gpp@main</code>, then create and run a starter project with <code>gpp init hello</code> and <code>gpp run</code>. The <a href="/guide/getting-started">getting started guide</a> walks through the setup.</p>
</details>

</div>

<footer class="catalog-footer">
<div class="catalog-footer-copy">
<p class="catalog-footer-eyebrow">Ready to try Go++?</p>
<h2>Build your first project</h2>
<p>Go++ compiles to ordinary Go, so you can keep the packages, tools, and deployment flow you already know.</p>
</div>
<a href="/guide/getting-started">Get started <span aria-hidden="true">→</span></a>
</footer>

---
layout: home

hero:
  text: Go++
  tagline: Gives Go superpowers — classes, polymorphism, overloading, multiple inheritance, exception handling, extension methods, serialization, annotations, records and more!
  image:
    src: /go-gopher-superman.png
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
<p>The compiler translates Go++ into ordinary Go. Go++ projects can use existing Go packages, and Go and Go++ source files can be built together. <a href="/guide/getting-started">Get started</a> or browse the <a href="/examples/">complete examples</a>.</p>
</details>

<details>
<summary>Is Go++ ready for production?</summary>
<p>Go++ is still an experiment and has not yet received broad community review. Its creator is not a compiler developer, so treat it as a project to explore and evaluate; don’t adopt it casually for production systems.</p>
</details>

<details>
<summary>What license applies to Go++?</summary>
<p>Go++ uses the same BSD 3-Clause license as Go. You can use, modify, and redistribute Go++ in source or binary form, including commercially, as long as you follow the license conditions. See the <a href="https://github.com/telgatech/gpp/blob/main/LICENSE">full license</a>. Some bundled third-party components have their own notices and licenses.</p>
</details>

<details>
<summary>Can I contribute?</summary>
<p>Yes. Questions, ideas, bug reports, and pull requests are welcome. For a larger feature, opening an issue first is a good way to discuss the design before investing in an implementation.</p>
</details>

<details>
<summary>How can I try Go++?</summary>
<p>Install the CLI with <code>go install github.com/telgatech/gpp@latest</code>, then create and run a starter project with <code>gpp init hello</code> and <code>gpp run .</code>. The <a href="/guide/getting-started">getting started guide</a> walks through the setup.</p>
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

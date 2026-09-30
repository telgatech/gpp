<script setup>
import DonationButton from './.vitepress/theme/DonationButton.vue'
</script>

# About Go++

<div class="about-profile-card">
  <img
    class="about-profile-avatar"
    src="https://media.licdn.com/dms/image/v2/D5603AQHyajRXSjE7ng/profile-displayphoto-shrink_200_200/profile-displayphoto-shrink_200_200/0/1722577204929?e=1792022400&amp;v=beta&amp;t=P_-cFL_q3ZyrVPjWY3AAEE3OalxXjkNv3cRup0Yrluk"
    alt="Sunder Rajan's LinkedIn profile photo"
    width="88"
    height="88"
    loading="lazy"
  />
  <div class="about-profile-copy">
    <h2>Sunder Rajan</h2>
    <p>Creator of Go++ · Telga Technologies</p>
    <div class="about-profile-socials">
      <a class="about-profile-social-button" href="https://x.com/sundersw" target="_blank" rel="noreferrer" aria-label="Sunder Rajan on X" title="X · @sundersw">
        <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M18.9 1.15h3.68l-8.04 9.19L24 22.85h-7.41l-5.8-7.58-6.64 7.58H.47l8.6-9.83L0 1.15h7.6l5.24 6.93zm-1.29 19.49h2.04L6.49 3.24H4.3z" /></svg>
      </a>
      <a class="about-profile-social-button" href="https://in.linkedin.com/in/sunder-rajan-1a4154171" target="_blank" rel="noreferrer" aria-label="Sunder Rajan on LinkedIn" title="LinkedIn">
        <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M20.45 20.45h-3.56v-5.57c0-1.33-.02-3.04-1.85-3.04-1.85 0-2.13 1.45-2.13 2.94v5.67H9.35V8.99h3.42v1.56h.05c.48-.9 1.64-1.85 3.37-1.85 3.6 0 4.27 2.37 4.27 5.46v6.29ZM5.34 7.43a2.06 2.06 0 1 1 0-4.12 2.06 2.06 0 0 1 0 4.12ZM7.12 20.45H3.56V8.99h3.56v11.46ZM22.23 0H1.77C.79 0 0 .77 0 1.72v20.56C0 23.23.79 24 1.77 24h20.45c.98 0 1.78-.77 1.78-1.72V1.72C24 .77 23.2 0 22.23 0Z" /></svg>
      </a>
    </div>
  </div>
</div>

## Why I created Go++

I've been programming in Go since the early 1.0 days. I love the productivity
it brings: the language is approachable, its tools are cohesive, and it is
refreshing to build software on a platform that feels so dependable. Go is a
remarkably reliable engineering achievement, and that is exactly why I want
to build on it rather than replace it.

Go's conservative approach to language features has helped keep the language
small, stable, and consistent. At the same time, years of application
development have left me (and countless others like me going by the feedback online)
wishing for less ceremony and more expressive ways to
handle some common patterns. That tension is where Go++ began: an experiment
to see whether those annoyances can be addressed while keeping Go's character,
toolchain, and interoperability intact.

Go++ explores that idea by compiling to ordinary Go, keeping existing Go
packages and tools part of the story.

## Experimental status

Go++ is an experiment to see how far Go can be pushed to address the major
annoyances people raise while preserving the language's character. I’m not a
hardened compiler developer, and this project has not yet been broadly vetted by the Go
community. Until it has received that scrutiny and earned confidence through
real-world use, please don’t adopt it casually for production systems.

## Consulting and project support

I’m open to consulting engagements. If you’re exploring Go++ or need help with
a software project, get in touch through [Telga](https://telga.in) or
[LinkedIn](https://in.linkedin.com/in/sunder-rajan-1a4154171).

I’m also open to donations to help keep Go++ moving. If you’d like to support
the project, you can contribute here:

<DonationButton />

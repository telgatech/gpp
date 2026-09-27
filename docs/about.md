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
    <a href="https://in.linkedin.com/in/sunder-rajan-1a4154171" target="_blank" rel="noreferrer">
      LinkedIn · See my other projects <span aria-hidden="true">→</span>
    </a>
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

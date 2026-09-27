<script setup>
import { onBeforeUnmount, onMounted, ref } from 'vue'

const form = ref(null)
let paymentButtonScript

onMounted(() => {
  paymentButtonScript = document.createElement('script')
  paymentButtonScript.src = 'https://checkout.razorpay.com/v1/payment-button.js'
  paymentButtonScript.setAttribute('data-payment_button_id', 'pl_Tgy6a0IM1LKQJV')
  paymentButtonScript.async = true
  form.value?.append(paymentButtonScript)
})

onBeforeUnmount(() => {
  paymentButtonScript?.remove()
})
</script>

<template>
  <form ref="form" class="donation-button"></form>
</template>

<style scoped>
.donation-button {
  min-height: 48px;
  margin-top: 16px;
}
</style>

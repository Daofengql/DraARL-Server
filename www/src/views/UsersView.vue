<script setup lang="ts">
import { ref } from 'vue'

const users = ref([])
</script>

<template>
  <v-container>
    <v-row>
      <v-col cols="12">
        <v-card title="用户管理">
          <v-card-text>
            <v-alert type="info" class="mb-4">
              用户管理功能开发中...
            </v-alert>
          </v-card-text>
        </v-card>
      </v-col>
    </v-row>
  </v-container>
</template>

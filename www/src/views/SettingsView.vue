<script setup lang="ts">
import { ref } from 'vue'

const settings = ref({
  systemName: 'NRL 火链',
  systemPort: '60050',
  webPort: '9000',
})
</script>

<template>
  <v-container>
    <v-row>
      <v-col cols="12">
        <v-card title="系统设置">
          <v-card-text>
            <v-alert type="info" class="mb-4">
              系统设置功能开发中...
            </v-alert>
          </v-card-text>
        </v-card>
      </v-col>
    </v-row>
  </v-container>
</template>

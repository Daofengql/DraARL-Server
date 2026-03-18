import {
  Box,
  Typography,
  IconButton,
  Button,
  Stack,
  useTheme,
  useMediaQuery,
} from '@mui/material'
import KeyboardArrowLeft from '@mui/icons-material/KeyboardArrowLeft'
import KeyboardArrowRight from '@mui/icons-material/KeyboardArrowRight'
import KeyboardDoubleArrowLeft from '@mui/icons-material/KeyboardDoubleArrowLeft'
import KeyboardDoubleArrowRight from '@mui/icons-material/KeyboardDoubleArrowRight'

interface PaginationProps {
  currentPage: number
  totalPages: number
  total: number
  pageSize: number
  onPageChange: (page: number) => void
}

export function Pagination({
  currentPage,
  totalPages,
  total,
  pageSize,
  onPageChange,
}: PaginationProps) {
  const theme = useTheme()
  const isMobile = useMediaQuery(theme.breakpoints.down('sm'))

  const startItem = (currentPage - 1) * pageSize + 1
  const endItem = Math.min(currentPage * pageSize, total)

  const getPageNumbers = () => {
    const pages: (number | string)[] = []
    const showPages = 5

    if (totalPages <= showPages) {
      for (let i = 1; i <= totalPages; i++) {
        pages.push(i)
      }
    } else {
      if (currentPage <= 3) {
        pages.push(1, 2, 3, 4, '...', totalPages)
      } else if (currentPage >= totalPages - 2) {
        pages.push(1, '...', totalPages - 3, totalPages - 2, totalPages - 1, totalPages)
      } else {
        pages.push(1, '...', currentPage - 1, currentPage, currentPage + 1, '...', totalPages)
      }
    }

    return pages
  }

  if (totalPages <= 1) return null

  return (
    <Box
      sx={{
        display: 'flex',
        flexDirection: { xs: 'column', sm: 'row' },
        alignItems: { sm: 'center' },
        justifyContent: 'space-between',
        gap: 2,
        mt: 3,
      }}
    >
      <Typography variant="body2" color="text.secondary">
        显示 {startItem} - {endItem} 条，共 {total} 条
      </Typography>

      <Stack spacing={1} direction="row" alignItems="center" flexWrap="wrap" justifyContent="center">
        <IconButton
          onClick={() => onPageChange(1)}
          disabled={currentPage === 1}
          size="small"
          title="第一页"
        >
          <KeyboardDoubleArrowLeft fontSize="small" />
        </IconButton>

        <Button
          onClick={() => onPageChange(currentPage - 1)}
          disabled={currentPage === 1}
          startIcon={<KeyboardArrowLeft />}
          size="small"
          variant="outlined"
        >
          {isMobile ? '' : '上一页'}
        </Button>

        {getPageNumbers().map((page, i) => (
          <Button
            key={i}
            onClick={() => typeof page === 'number' && onPageChange(page)}
            disabled={page === '...'}
            variant={page === currentPage ? 'contained' : 'outlined'}
            size="small"
            sx={{
              minWidth: 40,
              ...(page === '...' && {
                border: 'none',
                cursor: 'default',
                '&:hover': { bgcolor: 'transparent' },
              }),
            }}
          >
            {page}
          </Button>
        ))}

        <Button
          onClick={() => onPageChange(currentPage + 1)}
          disabled={currentPage === totalPages}
          endIcon={<KeyboardArrowRight />}
          size="small"
          variant="outlined"
        >
          {isMobile ? '' : '下一页'}
        </Button>

        <IconButton
          onClick={() => onPageChange(totalPages)}
          disabled={currentPage === totalPages}
          size="small"
          title="最后一页"
        >
          <KeyboardDoubleArrowRight fontSize="small" />
        </IconButton>
      </Stack>
    </Box>
  )
}

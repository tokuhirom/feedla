// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest'
import * as api from './client'

describe('ApiError message extraction', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('unwraps the {"error": ...} JSON body into a plain message', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        new Response('{"error":"feed: no feed found at or linked from x"}', {
          status: 502,
        }),
      ),
    )
    await expect(api.getMe()).rejects.toMatchObject({
      status: 502,
      message: 'feed: no feed found at or linked from x',
    })
  })

  it('keeps a non-JSON body verbatim', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(new Response('bad request', { status: 400 })),
    )
    await expect(api.getMe()).rejects.toMatchObject({
      status: 400,
      message: 'bad request',
    })
  })

  it('falls back to statusText for an empty body', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValue(
          new Response('', { status: 400, statusText: 'Bad Request' }),
        ),
    )
    await expect(api.getMe()).rejects.toMatchObject({
      status: 400,
      message: 'Bad Request',
    })
  })
})

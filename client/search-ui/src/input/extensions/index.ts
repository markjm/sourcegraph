/* eslint-disable jsdoc/check-indentation */
import { Extension, StateEffect, StateEffectType, StateField } from '@codemirror/state'
import { EditorView, ViewUpdate } from '@codemirror/view'
import { Observable } from 'rxjs'

import { createCancelableFetchSuggestions } from '@sourcegraph/shared/src/search/query/providers'
import { SearchMatch } from '@sourcegraph/shared/src/search/stream'

import { createDefaultSuggestionSources, searchQueryAutocompletion } from './completion'

export { createDefaultSuggestionSources, searchQueryAutocompletion }

/**
 * Creates an extension that calls the provided callback whenever the editor
 * content has changed.
 */
export const changeListener = (callback: (value: string) => void): Extension =>
    EditorView.updateListener.of((update: ViewUpdate) => {
        if (update.docChanged) {
            return callback(update.state.sliceDoc())
        }
    })

/**
 * Creates a search query suggestions extension with default suggestion sources
 * and cancable requests.
 */
export const createDefaultSuggestions = ({
    isSourcegraphDotCom,
    globbing,
    fetchSuggestions,
}: {
    isSourcegraphDotCom: boolean
    globbing: boolean
    fetchSuggestions: (query: string) => Observable<SearchMatch[]>
}): Extension =>
    searchQueryAutocompletion(
        createDefaultSuggestionSources({
            fetchSuggestions: createCancelableFetchSuggestions(fetchSuggestions),
            globbing,
            isSourcegraphDotCom,
        })
    )

/**
 * A helper function for creating an extension that operates on the value which
 * can be updated via an effect.
 * This is useful in React components where the extension depends on the value
 * of a prop  but that prop is unstable, and especially useful for callbacks.
 * Instead of reconfiguring the editor whenever the value changes (which is
 * apparently not cheap), the extension can be updated via the returned update
 * function or effect.
 *
 * Example:
 *
 * const {onChange} = props;
 * const [onChangeField, setOnChange] = useMemo(() => createUpdateableField(...), [])
 * ...
 * useEffect(() => {
 *   if (editor) {
 *     setOnchange(editor, onChange)
 *   }
 * }, [editor, onChange])
 */
export function createUpdateableField<T>(
    provider: (field: StateField<T>) => Extension,
    defaultValue?: T
): [StateField<T>, (editor: EditorView, newValue: typeof defaultValue) => void, StateEffectType<typeof defaultValue>] {
    const fieldEffect = StateEffect.define<typeof defaultValue>()
    const field = StateField.define<typeof defaultValue>({
        create() {
            return defaultValue
        },
        update(value, transaction) {
            const effect = transaction.effects.find((effect): effect is StateEffect<typeof defaultValue> =>
                effect.is(fieldEffect)
            )
            return effect ? effect.value : value
        },
        provide: provider,
    })

    return [field, (editor, newValue) => editor.dispatch({ effects: [fieldEffect.of(newValue)] }), fieldEffect]
}
